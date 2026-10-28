package command_test

import (
	"context"
	"errors"
	"testing"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

const startCash = 1_000_000_000

// engineEnv wires the real handlers to an in-memory repo that always contains
// the FEE_COLLECTOR account.
type engineEnv struct {
	m      *order.Matcher
	repo   *fakeAccountRepo
	place  *command.PlaceOrderHandler
	cancel *command.CancelOrderHandler
}

func newEngineEnv(accs ...*account.Account) *engineEnv {
	m := order.NewMatcher(order.NewOrderBook("APX"))
	repo := newFakeRepo(append(accs, account.NewAccount("FEE_COLLECTOR", 0))...)
	return newEngineEnvWith(m, repo)
}

func newEngineEnvWith(m *order.Matcher, repo *fakeAccountRepo) *engineEnv {
	return &engineEnv{
		m: m, repo: repo,
		place:  command.NewPlaceOrderHandler(m, repo, nil),
		cancel: command.NewCancelOrderHandler(m, repo, nil),
	}
}

func (e *engineEnv) acc(id string) *account.Account { return e.repo.accs[id] }

func (e *engineEnv) do(t *testing.T, acc string, side order.OrderSide, typ order.OrderType, price order.Price, qty order.Quantity) *command.PlaceOrderResult {
	t.Helper()
	res, err := e.place.Handle(context.Background(), command.PlaceOrderCommand{
		AccountID: acc, Symbol: "APX", Side: side, Type: typ, Price: price, Quantity: qty,
	})
	if err != nil {
		t.Fatalf("place %s %s %s %d x %d: %v", acc, side, typ, price, qty, err)
	}
	return res
}

func (e *engineEnv) limit(t *testing.T, acc string, side order.OrderSide, price order.Price, qty order.Quantity) *command.PlaceOrderResult {
	t.Helper()
	return e.do(t, acc, side, order.TypeLimit, price, qty)
}

func (e *engineEnv) cancelAs(acc, id string) (*order.Order, error) {
	return e.cancel.Handle(context.Background(), command.CancelOrderCommand{OrderID: id, AccountID: acc, Symbol: "APX"})
}

func trader(id string, shares uint32) *account.Account {
	a := account.NewAccount(id, startCash)
	a.CreditStock("APX", shares)
	return a
}

// 3.1 + 3.2 + 3.3 (T3): both sides settle, per-fill reservation release, fees to collector.
func TestSettlement_BothSidesAndCollector(t *testing.T) {
	e := newEngineEnv(trader("MAKER", 0), trader("TAKER", 1000))
	e.limit(t, "MAKER", order.SideBuy, 19500, 100) // R = 1_950_000 + 2_925 = 1_952_925
	maker := e.acc("MAKER")
	if maker.LockedCash != 1_952_925 {
		t.Fatalf("reserve = %d", maker.LockedCash)
	}

	e.limit(t, "TAKER", order.SideSell, 19500, 40) // n = 780_000, fee 1_170 each side

	// maker (resting BUY): cash debited with fee, shares credited, proportional unlock (40/100 of R).
	if got, want := maker.CashBalance, int64(startCash-781_170); got != want {
		t.Errorf("maker cash = %d, want %d", got, want)
	}
	if maker.Holdings["APX"] != 40 {
		t.Errorf("maker shares = %d, want 40", maker.Holdings["APX"])
	}
	if got, want := maker.LockedCash, int64(1_952_925-781_170); got != want {
		t.Errorf("maker locked = %d, want %d", got, want)
	}
	taker := e.acc("TAKER")
	if got, want := taker.CashBalance, int64(startCash+780_000-1_170); got != want {
		t.Errorf("taker cash = %d, want %d", got, want)
	}
	if taker.Holdings["APX"] != 960 || taker.LockedStocks["APX"] != 0 {
		t.Errorf("taker shares=%d locked=%d", taker.Holdings["APX"], taker.LockedStocks["APX"])
	}
	if got := e.acc("FEE_COLLECTOR").CashBalance; got != 2_340 {
		t.Errorf("collector cash = %d, want 2340 (both fees)", got)
	}
}

// 3.2: the maker is a SELL (taker BUY): maker cash/shares/unlock settle too.
func TestSettlement_MakerSell(t *testing.T) {
	e := newEngineEnv(trader("MAKER", 100), trader("BUYER", 0))
	e.limit(t, "MAKER", order.SideSell, 19500, 50)
	e.limit(t, "BUYER", order.SideBuy, 19500, 20) // n = 390_000, fee 585
	maker := e.acc("MAKER")
	if maker.Holdings["APX"] != 80 || maker.LockedStocks["APX"] != 30 {
		t.Errorf("maker shares=%d locked=%d, want 80/30", maker.Holdings["APX"], maker.LockedStocks["APX"])
	}
	if got, want := maker.CashBalance, int64(startCash+390_000-585); got != want {
		t.Errorf("maker cash = %d, want %d", got, want)
	}
	if got := e.acc("FEE_COLLECTOR").CashBalance; got != 1_170 {
		t.Errorf("collector = %d, want 1170", got)
	}
}

// 3.1: partial fill releases only its share; price improvement and rounding are
// released on the final fill.
func TestSettleBuy_PartialFillAndPriceImprovement(t *testing.T) {
	e := newEngineEnv(trader("MAKER", 100), trader("BUYER", 0), trader("S2", 100))
	e.limit(t, "MAKER", order.SideSell, 19500, 4)

	buyer := e.acc("BUYER")
	res := e.limit(t, "BUYER", order.SideBuy, 19600, 10) // R = 196_000 + 294 = 196_294
	if res.Order.Status != order.StatusPartiallyFilled {
		t.Fatalf("status = %s", res.Order.Status)
	}
	// 4 @ 19500: cost 78_000 + 117; unlock floor(196_294*4/10) = 78_517
	if got, want := buyer.CashBalance, int64(startCash-78_117); got != want {
		t.Errorf("cash = %d, want %d", got, want)
	}
	if got, want := buyer.LockedCash, int64(196_294-78_517); got != want {
		t.Errorf("locked after partial = %d, want %d (releasing the whole order would give 0)", got, want)
	}

	// final fill: 6 @ 19600 costs 117_600 + 177 = 117_777; whole remainder unlocks
	e.limit(t, "S2", order.SideSell, 19600, 6)
	if buyer.LockedCash != 0 {
		t.Errorf("locked after final fill = %d, want 0", buyer.LockedCash)
	}
	if got, want := buyer.CashBalance, int64(startCash-78_117-117_777); got != want {
		t.Errorf("cash = %d, want %d", got, want)
	}
	if buyer.Holdings["APX"] != 10 {
		t.Errorf("shares = %d", buyer.Holdings["APX"])
	}
}

// 3.1 price improvement on the final fill: limit 19600 filled at 19500 only.
func TestSettleBuy_PriceImprovementReleasedOnFinalFill(t *testing.T) {
	e := newEngineEnv(trader("MAKER", 100), trader("BUYER", 0))
	e.limit(t, "MAKER", order.SideSell, 19500, 10)
	res := e.limit(t, "BUYER", order.SideBuy, 19600, 10)
	if res.Order.Status != order.StatusFilled {
		t.Fatalf("status = %s", res.Order.Status)
	}
	buyer := e.acc("BUYER")
	if buyer.LockedCash != 0 {
		t.Errorf("locked = %d, want 0", buyer.LockedCash)
	}
	if got, want := buyer.CashBalance, int64(startCash-195_000-293); got != want { // fee ceil(292.5)=293
		t.Errorf("cash = %d, want %d", got, want)
	}
}

// 3.4: unfilled remainder of a MARKET BUY is released and the order is terminal.
func TestMarketBuy_RemainderReleased(t *testing.T) {
	e := newEngineEnv(trader("MAKER", 100), trader("BUYER", 0))
	e.limit(t, "MAKER", order.SideSell, 19500, 4)
	res := e.do(t, "BUYER", order.SideBuy, order.TypeMarket, 0, 10)
	buyer := e.acc("BUYER")
	if buyer.LockedCash != 0 {
		t.Errorf("locked = %d, leftover reservation leaked", buyer.LockedCash)
	}
	if res.Order.Status != order.StatusCanceled {
		t.Errorf("status = %s, want CANCELED", res.Order.Status)
	}
	if got, want := buyer.CashBalance, int64(startCash-78_117); got != want {
		t.Errorf("cash = %d, want %d", got, want)
	}
	if b, _ := depthLen(e.m); b != 0 {
		t.Error("market order must never rest")
	}
}

// 3.4: a MARKET SELL with no bids fills nothing; shares are released, status REJECTED.
func TestMarketSell_NoLiquidityRejectedAndReleased(t *testing.T) {
	e := newEngineEnv(trader("SELLER", 100))
	res := e.do(t, "SELLER", order.SideSell, order.TypeMarket, 0, 10)
	if res.Order.Status != order.StatusRejected {
		t.Errorf("status = %s, want REJECTED", res.Order.Status)
	}
	if got := e.acc("SELLER").LockedStocks["APX"]; got != 0 {
		t.Errorf("locked stocks = %d, want 0", got)
	}
}

// 3.5 T1/T2: a foreign cancel is a 404-equivalent and changes nothing; owner cancel works after it.
func TestCancel_OwnershipT1T2(t *testing.T) {
	e := newEngineEnv(trader("OWNER", 0), trader("ATTACKER", 0))
	owner := e.acc("OWNER")
	prior := owner.LockedCash
	res := e.limit(t, "OWNER", order.SideBuy, 19500, 10)
	locked := owner.LockedCash
	if locked == prior {
		t.Fatal("expected a reservation")
	}

	_, err := e.cancelAs("ATTACKER", res.Order.ID)
	if !errors.Is(err, common.ErrOrderNotFound) {
		t.Fatalf("foreign cancel err = %v, want ErrOrderNotFound (same as unknown order)", err)
	}
	if _, ok := e.m.Get(res.Order.ID); !ok {
		t.Fatal("foreign cancel removed the order")
	}
	if owner.LockedCash != locked || e.acc("ATTACKER").LockedCash != 0 {
		t.Errorf("foreign cancel moved locks: owner=%d attacker=%d", owner.LockedCash, e.acc("ATTACKER").LockedCash)
	}

	if _, err := e.cancelAs("OWNER", res.Order.ID); err != nil {
		t.Fatalf("owner cancel: %v", err)
	}
	if owner.LockedCash != prior {
		t.Errorf("locked_cash = %d after cancel, want prior %d", owner.LockedCash, prior)
	}
}

// 3.5: cancel of a partially filled BUY releases only what is still reserved, and SELL releases shares.
func TestCancel_PartiallyFilledReleasesRemainder(t *testing.T) {
	e := newEngineEnv(trader("OWNER", 100), trader("S", 100), trader("B", 0))
	buy := e.limit(t, "OWNER", order.SideBuy, 19500, 10)
	e.limit(t, "S", order.SideSell, 19500, 4)
	if _, err := e.cancelAs("OWNER", buy.Order.ID); err != nil {
		t.Fatal(err)
	}
	owner := e.acc("OWNER")
	if owner.LockedCash != 0 {
		t.Errorf("locked = %d, want 0", owner.LockedCash)
	}
	if got, want := owner.CashBalance, int64(startCash-78_117); got != want {
		t.Errorf("cash = %d, want %d", got, want)
	}

	sell := e.limit(t, "OWNER", order.SideSell, 20000, 10)
	e.limit(t, "B", order.SideBuy, 20000, 3)
	if _, err := e.cancelAs("OWNER", sell.Order.ID); err != nil {
		t.Fatal(err)
	}
	if got := owner.LockedStocks["APX"]; got != 0 {
		t.Errorf("locked stocks = %d, want 0", got)
	}
}

type errRepo struct {
	*fakeAccountRepo
	failID string
	err    error
}

func (r *errRepo) GetByID(ctx context.Context, id string) (*account.Account, error) {
	if id == r.failID {
		return nil, r.err
	}
	return r.fakeAccountRepo.GetByID(ctx, id)
}

// 3.6: a repository error is returned and the order is NOT silently removed.
func TestCancel_RepoErrorKeepsOrderAndIsReturned(t *testing.T) {
	m := order.NewMatcher(order.NewOrderBook("APX"))
	base := newFakeRepo(trader("OWNER", 0), account.NewAccount("FEE_COLLECTOR", 0))
	e := newEngineEnvWith(m, base)
	res := e.limit(t, "OWNER", order.SideBuy, 19500, 10)
	locked := e.acc("OWNER").LockedCash

	boom := errors.New("db down")
	failing := command.NewCancelOrderHandler(m, &errRepo{fakeAccountRepo: base, failID: "OWNER", err: boom}, nil)
	_, err := failing.Handle(context.Background(), command.CancelOrderCommand{OrderID: res.Order.ID, AccountID: "OWNER", Symbol: "APX"})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the repository error", err)
	}
	if _, ok := m.Get(res.Order.ID); !ok {
		t.Error("order removed although the account could not be resolved")
	}
	if e.acc("OWNER").LockedCash != locked {
		t.Error("lock changed on failed cancel")
	}
}

// 3.6: a missing account (repo returns nil, nil) is an error, never a silent skip.
func TestCancel_MissingAccountKeepsOrder(t *testing.T) {
	m := order.NewMatcher(order.NewOrderBook("APX"))
	base := newFakeRepo(trader("OWNER", 0), account.NewAccount("FEE_COLLECTOR", 0))
	e := newEngineEnvWith(m, base)
	res := e.limit(t, "OWNER", order.SideBuy, 19500, 10)
	delete(base.accs, "OWNER")
	if _, err := e.cancelAs("OWNER", res.Order.ID); err == nil {
		t.Fatal("expected an error for a missing account")
	}
	if _, ok := m.Get(res.Order.ID); !ok {
		t.Error("order removed although the account is missing")
	}
}

// 3.6: if the release cannot be applied (inconsistent lock), the order stays and the error surfaces.
func TestCancel_ReleaseFailureKeepsOrder(t *testing.T) {
	e := newEngineEnv(trader("OWNER", 0))
	res := e.limit(t, "OWNER", order.SideBuy, 19500, 10)
	e.acc("OWNER").LockedCash = 1 // corrupt: less than the order's reservation
	_, err := e.cancelAs("OWNER", res.Order.ID)
	if !errors.Is(err, common.ErrSettlementMismatch) {
		t.Fatalf("err = %v, want ErrSettlementMismatch", err)
	}
	if _, ok := e.m.Get(res.Order.ID); !ok {
		t.Error("order removed although release failed")
	}
}

// 3.3: a missing FEE_COLLECTOR fails loudly before anything is reserved.
func TestPlace_MissingFeeCollectorFailsLoudly(t *testing.T) {
	m := order.NewMatcher(order.NewOrderBook("APX"))
	repo := newFakeRepo(trader("A", 0))
	e := newEngineEnvWith(m, repo)
	_, err := e.place.Handle(context.Background(), command.PlaceOrderCommand{
		AccountID: "A", Symbol: "APX", Side: order.SideBuy, Type: order.TypeLimit, Price: 19500, Quantity: 1,
	})
	if err == nil {
		t.Fatal("expected an error without a FEE_COLLECTOR account")
	}
	if repo.accs["A"].LockedCash != 0 {
		t.Error("funds reserved despite the failure")
	}
	if b, _ := depthLen(m); b != 0 {
		t.Error("order rested despite the failure")
	}
}

// 3.7 T10: same-account orders never trade; the taker remainder is rejected, the maker untouched.
func TestSelfTrade_CancelNewest(t *testing.T) {
	e := newEngineEnv(trader("A", 100))
	bid := e.limit(t, "A", order.SideBuy, 19500, 10)
	before := e.acc("A").Snapshot()

	res := e.limit(t, "A", order.SideSell, 19500, 10)
	if len(res.Matches) != 0 {
		t.Fatalf("self trade executed: %+v", res.Matches)
	}
	if res.Order.Status != order.StatusRejected {
		t.Errorf("taker status = %s, want REJECTED", res.Order.Status)
	}
	if _, ok := e.m.Get(bid.Order.ID); !ok {
		t.Error("maker was removed")
	}
	if _, asks := depthLen(e.m); asks != 0 {
		t.Error("rejected taker rested in the book")
	}
	after := e.acc("A").Snapshot()
	if after.CashBalance != before.CashBalance || after.Holdings["APX"] != before.Holdings["APX"] ||
		after.LockedStocks["APX"] != 0 || after.LockedCash != before.LockedCash {
		t.Errorf("balances changed: before=%+v after=%+v", before, after)
	}
}

// 3.7: fills against other accounts ahead of the own order still happen.
func TestSelfTrade_PartialThenStop(t *testing.T) {
	e := newEngineEnv(trader("A", 100), trader("B", 0))
	e.limit(t, "B", order.SideBuy, 19500, 4)
	own := e.limit(t, "A", order.SideBuy, 19500, 10)
	res := e.limit(t, "A", order.SideSell, 19500, 10)
	if len(res.Matches) != 1 || res.Matches[0].Quantity != 4 || res.Matches[0].MakerAccountID != "B" {
		t.Fatalf("matches = %+v", res.Matches)
	}
	if res.Order.Status != order.StatusCanceled {
		t.Errorf("status = %s, want CANCELED (partially filled then stopped)", res.Order.Status)
	}
	if _, ok := e.m.Get(own.Order.ID); !ok {
		t.Error("own maker removed")
	}
	if got := e.acc("A").LockedStocks["APX"]; got != 0 {
		t.Errorf("locked stocks = %d, want 0", got)
	}
}

// 3.8: queue depth follows fills on the bid side and the ask side.
func TestDepth_TotalQuantityFollowsFills(t *testing.T) {
	e := newEngineEnv(trader("M1", 1000), trader("T", 1000))
	e.limit(t, "M1", order.SideBuy, 19500, 100)
	e.limit(t, "T", order.SideSell, 19500, 30)
	if d := e.m.Depth(5); len(d.Bids) != 1 || d.Bids[0].TotalQuantity != 70 {
		t.Fatalf("bid depth = %+v, want 70", d.Bids)
	}

	e.limit(t, "M1", order.SideSell, 20000, 100)
	e.limit(t, "T", order.SideBuy, 20000, 25)
	if d := e.m.Depth(5); len(d.Asks) != 1 || d.Asks[0].TotalQuantity != 75 {
		t.Fatalf("ask depth = %+v, want 75", d.Asks)
	}
}

// 3.8: after cancelling a partially filled maker the level equals the sum of the remaining orders.
func TestDepth_CancelPartiallyFilledMaker(t *testing.T) {
	e := newEngineEnv(trader("M1", 0), trader("M2", 0), trader("T", 1000))
	a := e.limit(t, "M1", order.SideBuy, 19500, 100)
	e.limit(t, "M2", order.SideBuy, 19500, 50)
	e.limit(t, "T", order.SideSell, 19500, 30) // fills 30 of A
	if _, err := e.cancelAs("M1", a.Order.ID); err != nil {
		t.Fatal(err)
	}
	d := e.m.Depth(5)
	if len(d.Bids) != 1 || d.Bids[0].TotalQuantity != 50 || d.Bids[0].OrderCount != 1 {
		t.Fatalf("bid depth = %+v, want one level of 50", d.Bids)
	}
}
