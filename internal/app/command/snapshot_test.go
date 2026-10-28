package command_test

import (
	"context"
	"sync"
	"testing"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

type fakeAccountRepo struct {
	mu      sync.Mutex
	accs    map[string]*account.Account
	saves   []*account.Account // detached copy of every Save, in call order
	saveErr func(call int, acc *account.Account) error
}

func newFakeRepo(accs ...*account.Account) *fakeAccountRepo {
	r := &fakeAccountRepo{accs: map[string]*account.Account{}}
	for _, a := range accs {
		r.accs[a.ID] = a
	}
	return r
}

func (r *fakeAccountRepo) GetByID(_ context.Context, id string) (*account.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.accs[id], nil
}

// Save records a detached copy of the account so tests can assert WHAT was
// persisted at each call, and fails when saveErr says so.
func (r *fakeAccountRepo) Save(_ context.Context, a *account.Account) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	call := len(r.saves)
	r.saves = append(r.saves, a.Snapshot())
	if r.saveErr != nil {
		return r.saveErr(call, a)
	}
	return nil
}

// Plan 1.4: the result of PlaceOrder must be a detached snapshot, not the live
// *Order that the matcher keeps mutating (and that the HTTP layer serialises).
func TestPlaceOrder_ReturnsDetachedOrderSnapshot(t *testing.T) {
	book := order.NewOrderBook("APX")
	m := order.NewMatcher(book)
	buyer := account.NewAccount("BUYER", 1_000_000_000)
	seller := account.NewAccount("SELLER", 0)
	seller.CreditStock("APX", 1000)
	repo := newFakeRepo(buyer, seller, account.NewAccount("FEE_COLLECTOR", 0))
	h := command.NewPlaceOrderHandler(m, repo, nil)

	res, err := h.Handle(context.Background(), command.PlaceOrderCommand{
		AccountID: "BUYER", Symbol: "APX", Side: order.SideBuy, Type: order.TypeLimit, Price: 19500, Quantity: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	live, ok := m.Get(res.Order.ID)
	if !ok {
		t.Fatal("order should rest in the book")
	}
	if live == res.Order {
		t.Fatal("PlaceOrder returned a pointer that Get also hands out; expected distinct snapshots")
	}

	// A later fill must not change what the caller already holds.
	if _, err := h.Handle(context.Background(), command.PlaceOrderCommand{
		AccountID: "SELLER", Symbol: "APX", Side: order.SideSell, Type: order.TypeLimit, Price: 19500, Quantity: 60,
	}); err != nil {
		t.Fatal(err)
	}
	if res.Order.FilledQuantity != 0 {
		t.Fatalf("returned order mutated after the fact: filled=%d", res.Order.FilledQuantity)
	}
}

// Plan 1.3: cancel goes through Matcher.Cancel and hands back a snapshot.
func TestCancelOrder_UsesMatcherAndReturnsSnapshot(t *testing.T) {
	book := order.NewOrderBook("APX")
	m := order.NewMatcher(book)
	buyer := account.NewAccount("BUYER", 1_000_000_000)
	repo := newFakeRepo(buyer, account.NewAccount("FEE_COLLECTOR", 0))
	place := command.NewPlaceOrderHandler(m, repo, nil)
	cancel := command.NewCancelOrderHandler(m, repo, nil)

	res, err := place.Handle(context.Background(), command.PlaceOrderCommand{
		AccountID: "BUYER", Symbol: "APX", Side: order.SideBuy, Type: order.TypeLimit, Price: 19500, Quantity: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := cancel.Handle(context.Background(), command.CancelOrderCommand{OrderID: res.Order.ID, AccountID: "BUYER", Symbol: "APX"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != order.StatusCanceled {
		t.Errorf("status = %v", got.Status)
	}
	if _, ok := m.Get(res.Order.ID); ok {
		t.Error("canceled order still resting")
	}
	if depth := m.Depth(10); len(depth.Bids) != 0 {
		t.Errorf("bids remain after cancel: %+v", depth.Bids)
	}
}
