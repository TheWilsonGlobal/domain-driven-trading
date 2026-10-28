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

// lastSave returns the most recent persisted copy of an account.
func lastSave(t *testing.T, r *fakeAccountRepo, id string) *account.Account {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.saves) - 1; i >= 0; i-- {
		if r.saves[i].ID == id {
			return r.saves[i]
		}
	}
	t.Fatalf("account %s was never saved", id)
	return nil
}

// 5.1: every state change of an account (reserve, settle both sides, release)
// is followed by accountRepo.Save, and the saved copy carries holdings and
// locked stocks, not just cash.
func TestPersist_ReserveSettleReleaseAreSaved(t *testing.T) {
	e := newEngineEnv(trader("MAKER", 0), trader("TAKER", 1000))

	buy := e.limit(t, "MAKER", order.SideBuy, 19500, 100)
	if got := lastSave(t, e.repo, "MAKER").LockedCash; got != buy.Order.RemainingReserved || got == 0 {
		t.Fatalf("reserve not saved: saved LockedCash=%d want %d", got, buy.Order.RemainingReserved)
	}

	e.limit(t, "TAKER", order.SideSell, 19500, 40) // fills 40 of the maker, both sides settle

	maker, taker, fee := lastSave(t, e.repo, "MAKER"), lastSave(t, e.repo, "TAKER"), lastSave(t, e.repo, account.FeeCollectorID)
	if maker.Holdings["APX"] != 40 {
		t.Errorf("maker saved holdings=%d want 40", maker.Holdings["APX"])
	}
	if taker.Holdings["APX"] != 960 || taker.LockedStocks["APX"] != 0 {
		t.Errorf("taker saved holdings=%d locked=%d want 960/0", taker.Holdings["APX"], taker.LockedStocks["APX"])
	}
	if taker.CashBalance <= startCash {
		t.Errorf("taker saved cash=%d, proceeds missing", taker.CashBalance)
	}
	if fee.CashBalance == 0 {
		t.Error("fee collector credit was not saved")
	}
	if maker.LockedCash != e.acc("MAKER").Snapshot().LockedCash {
		t.Errorf("saved maker lock %d differs from live %d", maker.LockedCash, e.acc("MAKER").LockedCash)
	}

	// a resting SELL locks stocks: the lock must reach the repository
	e.limit(t, "TAKER", order.SideSell, 19900, 50)
	if got := lastSave(t, e.repo, "TAKER").LockedStocks["APX"]; got != 50 {
		t.Errorf("saved locked stocks=%d want 50", got)
	}

	// release on cancel
	if _, err := e.cancelAs("MAKER", buy.Order.ID); err != nil {
		t.Fatal(err)
	}
	if got := lastSave(t, e.repo, "MAKER").LockedCash; got != 0 {
		t.Errorf("release not saved: LockedCash=%d want 0", got)
	}
}

// 5.1: a MARKET remainder is released and the release is saved.
func TestPersist_MarketRemainderReleaseIsSaved(t *testing.T) {
	e := newEngineEnv(trader("SELLER", 100), trader("BUYER", 0))
	e.limit(t, "SELLER", order.SideSell, 19500, 10)
	e.do(t, "BUYER", order.SideBuy, order.TypeMarket, 0, 50) // only 10 available
	if got := lastSave(t, e.repo, "BUYER").LockedCash; got != 0 {
		t.Errorf("saved BUYER LockedCash=%d want 0 after remainder release", got)
	}
}

// 5.1: a failing Save at reserve time is returned, the in-memory reservation is
// undone and the order never reaches the book.
func TestPersist_ReserveSaveErrorAbortsAndUndoes(t *testing.T) {
	boom := errors.New("db down")
	e := newEngineEnv(trader("A", 100))
	e.repo.saveErr = func(int, *account.Account) error { return boom }

	_, err := e.place.Handle(context.Background(), command.PlaceOrderCommand{
		AccountID: "A", Symbol: "APX", Side: order.SideBuy, Type: order.TypeLimit, Price: 19500, Quantity: 10,
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v, want the Save error", err)
	}
	if got := e.acc("A").LockedCash; got != 0 {
		t.Errorf("reservation not undone: LockedCash=%d", got)
	}
	if d := e.m.Depth(5); len(d.Bids) != 0 {
		t.Errorf("order reached the book: %+v", d.Bids)
	}

	_, err = e.place.Handle(context.Background(), command.PlaceOrderCommand{
		AccountID: "A", Symbol: "APX", Side: order.SideSell, Type: order.TypeLimit, Price: 19500, Quantity: 10,
	})
	if !errors.Is(err, boom) {
		t.Fatalf("sell err=%v, want the Save error", err)
	}
	if got := e.acc("A").LockedStocks["APX"]; got != 0 {
		t.Errorf("share lock not undone: %d", got)
	}
}

// 5.1: a Save failure during settlement is returned (never swallowed).
func TestPersist_SettleSaveErrorIsReturned(t *testing.T) {
	boom := errors.New("db down")
	e := newEngineEnv(trader("MAKER", 0), trader("TAKER", 100))
	e.limit(t, "MAKER", order.SideBuy, 19500, 10)
	calls := len(e.repo.saves)
	e.repo.saveErr = func(call int, _ *account.Account) error {
		if call >= calls+1 { // let the taker's reserve succeed, fail during settlement
			return boom
		}
		return nil
	}
	_, err := e.place.Handle(context.Background(), command.PlaceOrderCommand{
		AccountID: "TAKER", Symbol: "APX", Side: order.SideSell, Type: order.TypeLimit, Price: 19500, Quantity: 10,
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v, want the Save error", err)
	}
}

// 5.1: cancel returns a Save failure, keeps the order resting and the lock intact.
func TestPersist_CancelSaveErrorKeepsOrderAndLock(t *testing.T) {
	boom := errors.New("db down")
	e := newEngineEnv(trader("A", 0))
	res := e.limit(t, "A", order.SideBuy, 19500, 10)
	locked := e.acc("A").LockedCash
	e.repo.saveErr = func(int, *account.Account) error { return boom }

	if _, err := e.cancelAs("A", res.Order.ID); !errors.Is(err, boom) {
		t.Fatalf("err=%v, want the Save error", err)
	}
	if _, ok := e.m.Get(res.Order.ID); !ok {
		t.Error("order left the book although the release could not be saved")
	}
	if e.acc("A").LockedCash != locked {
		t.Errorf("lock changed on failed cancel: %d != %d", e.acc("A").LockedCash, locked)
	}
}

// cashReserveRepo adds the atomic conditional reserve of plan 5.1 to the fake.
type cashReserveRepo struct {
	*fakeAccountRepo
	calls   []int64
	failure error
}

func (r *cashReserveRepo) ReserveCash(_ context.Context, id string, amount int64) error {
	r.calls = append(r.calls, amount)
	return r.failure
}

// 5.1: when the repository offers ReserveCash the BUY reservation goes through
// it (the conditional UPDATE), and a refusal rolls the in-memory lock back.
func TestPersist_BuyReserveUsesAtomicReserveCash(t *testing.T) {
	base := newFakeRepo(trader("A", 0), account.NewAccount(account.FeeCollectorID, 0))
	repo := &cashReserveRepo{fakeAccountRepo: base}
	m := order.NewMatcher(order.NewOrderBook("APX"))
	h := command.NewPlaceOrderHandler(m, repo, nil)
	cmd := command.PlaceOrderCommand{AccountID: "A", Symbol: "APX", Side: order.SideBuy, Type: order.TypeLimit, Price: 19500, Quantity: 10}

	res, err := h.Handle(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.calls) != 1 || repo.calls[0] != res.Order.ReservedAmount {
		t.Fatalf("ReserveCash calls=%v want [%d]", repo.calls, res.Order.ReservedAmount)
	}

	repo.failure = common.ErrInsufficientBalance
	before := base.accs["A"].Snapshot().LockedCash
	if _, err := h.Handle(context.Background(), cmd); !errors.Is(err, common.ErrInsufficientBalance) {
		t.Fatalf("err=%v want ErrInsufficientBalance", err)
	}
	if got := base.accs["A"].Snapshot().LockedCash; got != before {
		t.Errorf("lock %d after refused reserve, want %d", got, before)
	}
}
