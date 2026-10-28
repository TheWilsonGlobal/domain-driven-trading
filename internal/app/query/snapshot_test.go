package query_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/query"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

type repo struct{ acc *account.Account }

func (r repo) GetByID(context.Context, string) (*account.Account, error) { return r.acc, nil }
func (r repo) Save(context.Context, *account.Account) error              { return nil }

// Plan 1.4: GetBalance must return a detached copy taken under the account lock.
// Serialising the live account while another request reserves funds is a data race.
func TestGetBalance_ReturnsDetachedSnapshotUnderConcurrency(t *testing.T) {
	live := account.NewAccount("ACC", 1_000_000_000)
	live.CreditStock("APX", 10)
	h := query.NewGetBalanceHandler(repo{live})

	got, err := h.Handle(context.Background(), query.GetBalanceQuery{AccountID: "ACC"})
	if err != nil {
		t.Fatal(err)
	}
	var _ *query.BalanceView = got // read DTO owned by the query package, not *account.Account
	if got.AccountID != "ACC" || got.CashBalance != 1_000_000_000 || got.Holdings["APX"] != 10 {
		t.Fatalf("view = %+v", got)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			if _, err := live.ReserveBuyFunds(100, 1); err == nil {
				live.ReleaseBuyFunds(100 + 0)
			}
			live.CreditStock("APX", 1)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			s, _ := h.Handle(context.Background(), query.GetBalanceQuery{AccountID: "ACC"})
			_ = s.Holdings["APX"]
			_ = s.LockedCash
		}
	}()
	wg.Wait()

	before := got.Holdings["APX"]
	live.CreditStock("APX", 5)
	if got.Holdings["APX"] != before {
		t.Error("snapshot holdings map aliases the live map")
	}
}

func TestGetOrderBook_DefaultsLevelsViaMatcher(t *testing.T) {
	m := order.NewMatcher(order.NewOrderBook("APX"))
	o, _ := order.NewOrder("O1", "", "A", "APX", order.SideBuy, order.TypeLimit, 19500, 5)
	if _, err := m.ProcessOrder(o); err != nil {
		t.Fatal(err)
	}
	d, err := query.NewGetOrderBookHandler(m).Handle(context.Background(), query.GetOrderBookQuery{Symbol: "APX"})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Bids) != 1 {
		t.Fatalf("bids = %+v", d.Bids)
	}
}

func TestBalanceView_JSONShapeIsStable(t *testing.T) {
	live := account.NewAccount("ACC", 100)
	h := query.NewGetBalanceHandler(repo{live})
	v, err := h.Handle(context.Background(), query.GetBalanceQuery{AccountID: "ACC"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"account_id", "cash_balance", "locked_cash", "holdings", "locked_stocks", "updated_at"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing json key %q in %s", k, b)
		}
	}
}
