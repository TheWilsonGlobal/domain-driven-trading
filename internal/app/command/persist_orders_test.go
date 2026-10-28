package command_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

// fakeOrderRepo stores detached copies and lists open orders in first-save order.
type fakeOrderRepo struct {
	mu      sync.Mutex
	orders  map[string]*order.Order
	seq     []string
	saveErr error
}

func newFakeOrders() *fakeOrderRepo { return &fakeOrderRepo{orders: map[string]*order.Order{}} }

func (r *fakeOrderRepo) GetByID(_ context.Context, id string) (*order.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[id]
	if !ok {
		return nil, common.ErrOrderNotFound
	}
	return o.Snapshot(), nil
}

func (r *fakeOrderRepo) Save(_ context.Context, o *order.Order) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saveErr != nil {
		return r.saveErr
	}
	if _, ok := r.orders[o.ID]; !ok {
		r.seq = append(r.seq, o.ID)
	}
	r.orders[o.ID] = o.Snapshot()
	return nil
}

func (r *fakeOrderRepo) ListOpen(_ context.Context, sym order.Symbol) ([]*order.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*order.Order
	for _, id := range r.seq {
		if o := r.orders[id]; o.Symbol == sym && o.IsOpen() {
			out = append(out, o.Snapshot())
		}
	}
	return out, nil
}

// fakeJournal behaves like the Postgres adapter: all items or none.
type fakeJournal struct {
	mu    sync.Mutex
	items []ports.TradeWithJournal
	err   error
}

func (j *fakeJournal) SaveTradesWithJournal(_ context.Context, items []ports.TradeWithJournal) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.err != nil {
		return j.err
	}
	j.items = append(j.items, items...)
	return nil
}

type persistEnv struct {
	*engineEnv
	orders  *fakeOrderRepo
	journal *fakeJournal
}

func newPersistEnv(accs ...*account.Account) *persistEnv {
	e := newEngineEnv(accs...)
	p := &persistEnv{engineEnv: e, orders: newFakeOrders(), journal: &fakeJournal{}}
	e.place.WithOrderStore(p.orders, p.journal)
	e.cancel.WithOrderStore(p.orders)
	return p
}

// 5.3: orders are persisted on place, fill and cancel with status, remaining
// quantity and both reservation fields.
func TestPersistOrders_PlaceFillCancel(t *testing.T) {
	e := newPersistEnv(trader("MAKER", 0), trader("TAKER", 1000))
	buy := e.limit(t, "MAKER", order.SideBuy, 19500, 100)

	stored, err := e.orders.GetByID(context.Background(), buy.Order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != order.StatusNew || stored.Quantity != 100 || stored.ReservedAmount == 0 ||
		stored.RemainingReserved != stored.ReservedAmount {
		t.Fatalf("placed order stored as %+v", stored)
	}

	sell := e.limit(t, "TAKER", order.SideSell, 19500, 40)
	maker, _ := e.orders.GetByID(context.Background(), buy.Order.ID)
	live, _ := e.m.Get(buy.Order.ID)
	if maker.Status != order.StatusPartiallyFilled || maker.FilledQuantity != 40 ||
		maker.RemainingReserved != live.RemainingReserved || maker.ReservedAmount != live.ReservedAmount {
		t.Fatalf("maker after fill stored %+v, live %+v", maker, live)
	}
	taker, _ := e.orders.GetByID(context.Background(), sell.Order.ID)
	if taker.Status != order.StatusFilled || taker.FilledQuantity != 40 {
		t.Fatalf("taker stored %+v", taker)
	}

	if _, err := e.cancelAs("MAKER", buy.Order.ID); err != nil {
		t.Fatal(err)
	}
	maker, _ = e.orders.GetByID(context.Background(), buy.Order.ID)
	if maker.Status != order.StatusCanceled {
		t.Fatalf("canceled order stored as %s", maker.Status)
	}
	if open, _ := e.orders.ListOpen(context.Background(), "APX"); len(open) != 0 {
		t.Fatalf("open orders after cancel/fill: %d", len(open))
	}
}

// 5.3: every execution is journalled with the trade; debits == credits per
// transaction and per asset, FEE_COLLECTOR is credited with both fees.
func TestPersistJournal_BalancedAndFeeCollectorCredited(t *testing.T) {
	e := newPersistEnv(trader("M1", 0), trader("M2", 0), trader("TAKER", 1000))
	e.limit(t, "M1", order.SideBuy, 19500, 30)
	e.limit(t, "M2", order.SideBuy, 19490, 30)
	res := e.limit(t, "TAKER", order.SideSell, 19490, 50) // sweeps two makers, price improvement for the taker

	if len(e.journal.items) != len(res.Matches) || len(res.Matches) != 2 {
		t.Fatalf("journal items=%d matches=%d, want 2", len(e.journal.items), len(res.Matches))
	}
	var collector int64
	for i, it := range e.journal.items {
		if it.Trade.TradeID != res.Matches[i].TradeID || it.Journal.TradeID != it.Trade.TradeID {
			t.Errorf("item %d: trade id mismatch %s / %s", i, it.Trade.TradeID, it.Journal.TradeID)
		}
		sums := map[account.AssetType]map[account.EntryType]int64{account.AssetCash: {}, account.AssetStock: {}}
		for _, en := range it.Journal.Entries {
			sums[en.AssetType][en.Type] += en.Amount
			if en.AccountID == account.FeeCollectorID && en.AssetType == account.AssetCash && en.Type == account.Debit {
				collector += en.Amount
			}
		}
		for asset, s := range sums {
			if s[account.Debit] != s[account.Credit] || s[account.Debit] == 0 {
				t.Errorf("tx %s %s: debit %d credit %d", it.Journal.ID, asset, s[account.Debit], s[account.Credit])
			}
		}
	}
	if got := e.acc(account.FeeCollectorID).Snapshot().CashBalance; got != collector || got == 0 {
		t.Errorf("collector balance %d, journal credits %d", got, collector)
	}
}

// 5.3: a failing journal write is returned, never swallowed.
func TestPersistJournal_ErrorIsReturned(t *testing.T) {
	boom := errors.New("tx failed")
	e := newPersistEnv(trader("M", 0), trader("T", 100))
	e.limit(t, "M", order.SideBuy, 19500, 10)
	e.journal.err = boom
	_, err := e.place.Handle(context.Background(), command.PlaceOrderCommand{
		AccountID: "T", Symbol: "APX", Side: order.SideSell, Type: order.TypeLimit, Price: 19500, Quantity: 10,
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v, want the journal error", err)
	}
}

// 5.3: an order-store failure on cancel keeps the order resting and the lock.
func TestPersistOrders_CancelSaveErrorKeepsOrder(t *testing.T) {
	boom := errors.New("orders down")
	e := newPersistEnv(trader("A", 0))
	res := e.limit(t, "A", order.SideBuy, 19500, 10)
	locked := e.acc("A").Snapshot().LockedCash
	e.orders.saveErr = boom
	if _, err := e.cancelAs("A", res.Order.ID); !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
	if _, ok := e.m.Get(res.Order.ID); !ok || e.acc("A").Snapshot().LockedCash != locked {
		t.Error("order or lock changed although the cancel failed")
	}
}

// restart simulates a process restart: accounts come back from the store as
// detached copies, the book is empty until RecoverBook fills it.
func (e *persistEnv) restart(t *testing.T) (*order.Matcher, *fakeAccountRepo, command.RecoveryReport) {
	t.Helper()
	var accs []*account.Account
	for _, a := range e.repo.accs {
		accs = append(accs, a.Snapshot())
	}
	repo := newFakeRepo(accs...)
	m := order.NewMatcher(order.NewOrderBook("APX"))
	rep, err := command.RecoverBook(context.Background(), m, e.orders, repo, nil)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	return m, repo, rep
}

type depthLevels struct{ Bids, Asks []order.PriceLevelDepth }

func depthOf(m *order.Matcher) depthLevels {
	d := m.Depth(100)
	return depthLevels{d.Bids, d.Asks}
}

// 5.4: after a restart the book (depth, FIFO) and the locks are exactly what
// they were, conservation holds, and the recovered engine keeps trading.
func TestRecovery_RebuildsBookLocksAndConservation(t *testing.T) {
	e := newPersistEnv(trader("A", 5000), trader("B", 5000), trader("C", 5000))
	e.limit(t, "A", order.SideBuy, 19500, 100)
	e.limit(t, "B", order.SideBuy, 19500, 50)
	e.limit(t, "C", order.SideBuy, 19400, 70)
	first := e.limit(t, "A", order.SideSell, 20000, 80)
	e.limit(t, "B", order.SideSell, 20000, 20)
	e.limit(t, "C", order.SideSell, 19600, 30)
	e.limit(t, "C", order.SideSell, 19500, 120) // crosses the 19500 bids in FIFO order and rests the remainder
	gone := e.limit(t, "B", order.SideBuy, 19300, 40)
	if _, err := e.cancelAs("B", gone.Order.ID); err != nil {
		t.Fatal(err)
	}

	wantDepth := depthOf(e.m)
	var cash0 int64
	var shares0 uint32
	locks := map[string][2]uint32{}
	lockedCash := map[string]int64{}
	for id, a := range e.repo.accs {
		s := a.Snapshot()
		cash0 += s.CashBalance
		shares0 += s.Holdings["APX"]
		locks[id] = [2]uint32{s.LockedStocks["APX"], 0}
		lockedCash[id] = s.LockedCash
	}

	m, repo, rep := e.restart(t)
	if rep.Restored == 0 {
		t.Fatal("nothing restored")
	}
	if rep.LocksCorrected != 0 {
		t.Errorf("consistent store needed %d lock corrections", rep.LocksCorrected)
	}
	if got := depthOf(m); !reflect.DeepEqual(got, wantDepth) {
		t.Fatalf("depth after recovery\n got %+v\nwant %+v", got, wantDepth)
	}
	var cash1 int64
	var shares1 uint32
	for id, a := range repo.accs {
		s := a.Snapshot()
		cash1 += s.CashBalance
		shares1 += s.Holdings["APX"]
		if s.LockedCash != lockedCash[id] || s.LockedStocks["APX"] != locks[id][0] {
			t.Errorf("%s locks cash=%d stocks=%d, want %d/%d", id, s.LockedCash, s.LockedStocks["APX"], lockedCash[id], locks[id][0])
		}
	}
	if cash0 != cash1 || shares0 != shares1 {
		t.Errorf("conservation: cash %d->%d shares %d->%d", cash0, cash1, shares0, shares1)
	}
	if _, ok := m.Get(first.Order.ID); !ok {
		t.Error("resting ask missing after recovery")
	}

	// FIFO survives: a sell at 19500 hits the older bid (B... first resting at that price) first.
	env2 := newEngineEnvWith(m, repo)
	env2.place.WithOrderStore(e.orders, e.journal)
	env2.cancel.WithOrderStore(e.orders)
	res := env2.limit(t, "C", order.SideSell, 19500, 10)
	if len(res.Matches) != 1 || res.Matches[0].MakerAccountID != e.firstBidAt19500(t) {
		t.Fatalf("recovered FIFO broken: %+v", res.Matches)
	}
	// and a restored order can be canceled, releasing its lock
	if _, err := env2.cancelAs("C", first.Order.ID); err == nil {
		t.Error("C canceled A's order")
	}
	if _, err := env2.cancelAs("A", first.Order.ID); err != nil {
		t.Fatalf("cancel restored order: %v", err)
	}
	if got := repo.accs["A"].Snapshot().LockedStocks["APX"]; got != 0 {
		t.Errorf("restored SELL lock not released: %d", got)
	}
}

// firstBidAt19500 returns the owner of the oldest still-open bid at 19500.
func (e *persistEnv) firstBidAt19500(t *testing.T) string {
	t.Helper()
	open, _ := e.orders.ListOpen(context.Background(), "APX")
	for _, o := range open {
		if o.Side == order.SideBuy && o.Price == 19500 {
			return o.AccountID
		}
	}
	t.Fatal("no bid at 19500")
	return ""
}

// 5.4: locks are recomputed from the open orders, so a store that crashed
// between the order write and the account write is healed on start.
func TestRecovery_RecomputesDriftedLocks(t *testing.T) {
	e := newPersistEnv(trader("A", 100), trader("B", 0))
	buy := e.limit(t, "B", order.SideBuy, 19500, 10)
	e.limit(t, "A", order.SideSell, 20000, 30)
	var accs []*account.Account
	for _, a := range e.repo.accs {
		s := a.Snapshot()
		// the account writes of the reservations were lost: balances intact, no locks
		lost, err := account.Restore(s.ID, s.CashBalance, 0, s.Holdings, nil, s.UpdatedAt)
		if err != nil {
			t.Fatal(err)
		}
		accs = append(accs, lost)
	}
	repo := newFakeRepo(accs...)
	m := order.NewMatcher(order.NewOrderBook("APX"))
	rep, err := command.RecoverBook(context.Background(), m, e.orders, repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.LocksCorrected != 2 {
		t.Errorf("LocksCorrected=%d want 2", rep.LocksCorrected)
	}
	if got := repo.accs["B"].Snapshot().LockedCash; got != buy.Order.RemainingReserved || got == 0 {
		t.Errorf("B lock %d want %d", got, buy.Order.RemainingReserved)
	}
	if got := repo.accs["A"].Snapshot().LockedStocks["APX"]; got != 30 {
		t.Errorf("A stock lock %d want 30", got)
	}
	if len(repo.saves) == 0 {
		t.Error("corrected accounts were not saved")
	}
}

// 5.4: an open order whose account is gone is a startup error, not a skip.
func TestRecovery_MissingAccountFails(t *testing.T) {
	e := newPersistEnv(trader("A", 0))
	e.limit(t, "A", order.SideBuy, 19500, 10)
	repo := newFakeRepo(account.NewAccount(account.FeeCollectorID, 0))
	_, err := command.RecoverBook(context.Background(), order.NewMatcher(order.NewOrderBook("APX")), e.orders, repo, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
}
