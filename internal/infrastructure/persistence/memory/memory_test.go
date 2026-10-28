package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

func TestOrderRepository_DetachedCopiesAndTimePriority(t *testing.T) {
	r := NewOrderRepository()
	ctx := context.Background()
	mk := func(id string) *order.Order {
		o, _ := order.NewOrder(id, "", "A", "APX", order.SideBuy, order.TypeLimit, 19500, 10)
		return o
	}
	first, second := mk("O1"), mk("O2")
	_ = r.Save(ctx, first)
	_ = r.Save(ctx, second)
	first.ApplyFill(3) // later update must not change its place in the queue
	_ = r.Save(ctx, first)

	got, _ := r.ListOpen(ctx, "APX")
	if len(got) != 2 || got[0].ID != "O1" || got[0].FilledQuantity != 3 {
		t.Fatalf("ListOpen = %+v", got)
	}
	got[0].FilledQuantity = 99 // mutating a result must not change the store
	again, _ := r.GetByID(ctx, "O1")
	if again.FilledQuantity != 3 {
		t.Fatal("store leaked its internal order")
	}
	first.ApplyFill(7)
	_ = r.Save(ctx, first)
	if open, _ := r.ListOpen(ctx, "APX"); len(open) != 1 || open[0].ID != "O2" {
		t.Fatalf("filled order still open: %+v", open)
	}
	if _, err := r.GetByID(ctx, "NOPE"); !errors.Is(err, common.ErrOrderNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestTradeJournal_AllOrNothing(t *testing.T) {
	r := NewTradeJournalRepository()
	good, _ := account.CreateTradeSettlementJournal("T1", "B", "S", account.FeeCollectorID, "APX", 19500, 3)
	bad, _ := account.CreateTradeSettlementJournal("T2", "B", "S", account.FeeCollectorID, "APX", 19500, 3)
	bad.Entries[2].Amount++
	items := []ports.TradeWithJournal{
		{Trade: order.OrderMatchedEvent{TradeID: "T1"}, Journal: good},
		{Trade: order.OrderMatchedEvent{TradeID: "T2"}, Journal: bad},
	}
	if err := r.SaveTradesWithJournal(context.Background(), items); !errors.Is(err, common.ErrUnbalancedLedger) {
		t.Fatalf("err=%v", err)
	}
	if tr, j := r.Counts(); tr != 0 || j != 0 {
		t.Fatalf("partial write: %d trades %d journals", tr, j)
	}
	if err := r.SaveTradesWithJournal(context.Background(), items[:1]); err != nil {
		t.Fatal(err)
	}
	if tr, j := r.Counts(); tr != 1 || j != 1 {
		t.Fatalf("counts %d/%d", tr, j)
	}
}
