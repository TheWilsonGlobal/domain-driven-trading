package query_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/query"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

// Plan 2.6: levels clamped to 1..50, symbol honoured or rejected.
func TestGetOrderBook_LevelsClampedAndSymbolChecked(t *testing.T) {
	m := order.NewMatcher(order.NewOrderBook("APX"))
	for i := 0; i < 80; i++ {
		o, _ := order.NewOrder(fmt.Sprintf("O%d", i), "", "A", "APX", order.SideBuy, order.TypeLimit, order.Price(10000+i*10), 1)
		if _, err := m.ProcessOrder(o); err != nil {
			t.Fatal(err)
		}
	}
	h := query.NewGetOrderBookHandler(m)
	for levels, want := range map[int]int{0: 10, -3: 10, 1: 1, 25: 25, 50: 50, 51: 50, 1 << 30: 50} {
		d, err := h.Handle(context.Background(), query.GetOrderBookQuery{Symbol: "APX", MaxLevels: levels})
		if err != nil || len(d.Bids) != want {
			t.Errorf("levels=%d: got %d bids err=%v, want %d", levels, len(d.Bids), err, want)
		}
	}
	if _, err := h.Handle(context.Background(), query.GetOrderBookQuery{Symbol: "ZZZ", MaxLevels: 5}); !errors.Is(err, common.ErrSymbolMismatch) {
		t.Errorf("unknown symbol err = %v", err)
	}
	if d, err := h.Handle(context.Background(), query.GetOrderBookQuery{MaxLevels: 5}); err != nil || d.Symbol != "APX" {
		t.Errorf("empty symbol: %v %v", d.Symbol, err)
	}
}
