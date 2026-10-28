package order

import (
	"fmt"
	"testing"
)

func TestMatcher_PartialFillsAndSpread(t *testing.T) {
	symbol := Symbol("HPG")
	ob := NewOrderBook(symbol)
	matcher := NewMatcher(ob)

	// Maker asks: SELL 500 @ 28,000
	makerSell, _ := NewOrder("M-ASK-1", "C-ASK-1", "ACC-S1", symbol, SideSell, TypeLimit, 28000, 500)
	_, _ = matcher.ProcessOrder(makerSell)

	// Taker buy: BUY 200 @ 28,000 (Partial Fill of Maker)
	takerBuy1, _ := NewOrder("T-BUY-1", "C-BUY-1", "ACC-B1", symbol, SideBuy, TypeLimit, 28000, 200)
	events1, _ := matcher.ProcessOrder(takerBuy1)

	if len(events1) != 1 {
		t.Fatalf("expected 1 match event, got %d", len(events1))
	}
	if events1[0].Quantity != 200 {
		t.Errorf("expected 200 shares matched, got %d", events1[0].Quantity)
	}
	if makerSell.RemainingQuantity() != 300 {
		t.Errorf("expected maker remaining 300, got %d", makerSell.RemainingQuantity())
	}
	if makerSell.Status != StatusPartiallyFilled {
		t.Errorf("expected status PARTIALLY_FILLED, got %s", makerSell.Status)
	}

	// Taker buy 2: BUY 400 @ 28,000 (Sweeps remaining 300 maker, 100 rests as BUY bid)
	takerBuy2, _ := NewOrder("T-BUY-2", "C-BUY-2", "ACC-B2", symbol, SideBuy, TypeLimit, 28000, 400)
	events2, _ := matcher.ProcessOrder(takerBuy2)

	if len(events2) != 1 {
		t.Fatalf("expected 1 match event, got %d", len(events2))
	}
	if events2[0].Quantity != 300 {
		t.Errorf("expected 300 shares matched, got %d", events2[0].Quantity)
	}
	if makerSell.Status != StatusFilled {
		t.Errorf("expected maker to be FILLED, got %s", makerSell.Status)
	}

	// Taker 2 should now have 100 remaining resting in book at BID 28,000
	if takerBuy2.RemainingQuantity() != 100 {
		t.Errorf("expected taker 2 remaining 100, got %d", takerBuy2.RemainingQuantity())
	}
	bestBidPrice, bidQty, exists := matcher.BestBid()
	if !exists || bestBidPrice != 28000 || bidQty != 100 {
		t.Errorf("expected best bid 28000 with 100 shares, got exists=%v price=%d qty=%d", exists, bestBidPrice, bidQty)
	}
}

func BenchmarkMatcher_LimitOrderMatching(b *testing.B) {
	symbol := Symbol("APX")
	ob := NewOrderBook(symbol)
	matcher := NewMatcher(ob)

	// Pre-fill book with 10,000 resting asks
	for i := 0; i < 10000; i++ {
		price := Price(20000 + (i%50)*10)
		order, _ := NewOrder(
			fmt.Sprintf("ASK-%d", i),
			fmt.Sprintf("C-ASK-%d", i),
			"ACC-MM",
			symbol,
			SideSell,
			TypeLimit,
			price,
			100,
		)
		_, _ = matcher.ProcessOrder(order)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		taker, _ := NewOrder(
			fmt.Sprintf("BENCH-BUY-%d", i),
			fmt.Sprintf("C-BENCH-%d", i),
			"ACC-TRADER",
			symbol,
			SideBuy,
			TypeLimit,
			20500,
			100,
		)
		_, _ = matcher.ProcessOrder(taker)
	}
}

// Plan 3.1: each fill consumes a proportional share of the reservation and the
// last fill consumes whatever is left (rounding, price improvement).
func TestApplyFill_ProportionalReservation(t *testing.T) {
	o, err := NewOrder("O1", "", "A", "APX", SideBuy, TypeLimit, 100, 10)
	if err != nil {
		t.Fatal(err)
	}
	o.Reserve(1001)
	if got := o.ApplyFill(3); got != 300 { // floor(1001*3/10)
		t.Errorf("share 1 = %d, want 300", got)
	}
	if got := o.ApplyFill(3); got != 300 { // floor(701*3/7)
		t.Errorf("share 2 = %d, want 300", got)
	}
	if got := o.ApplyFill(4); got != 401 {
		t.Errorf("final share = %d, want the whole remainder 401", got)
	}
	if o.RemainingReserved != 0 || o.Status != StatusFilled {
		t.Errorf("remaining=%d status=%s", o.RemainingReserved, o.Status)
	}
}

// Plan 3.8: the queue total follows fills, not only removals.
func TestOrderQueue_FillKeepsTotal(t *testing.T) {
	q := NewOrderQueue(100)
	a, _ := NewOrder("A", "", "X", "APX", SideBuy, TypeLimit, 100, 10)
	b, _ := NewOrder("B", "", "Y", "APX", SideBuy, TypeLimit, 100, 5)
	q.Enqueue(a)
	q.Enqueue(b)
	a.ApplyFill(4)
	q.Fill(4)
	if q.TotalQuantity != 11 {
		t.Fatalf("total = %d, want 11", q.TotalQuantity)
	}
	q.Remove(a)
	if q.TotalQuantity != 5 {
		t.Fatalf("total after removing partially filled maker = %d, want 5", q.TotalQuantity)
	}
}

// Plan 3.7: a taker never trades with its own account's resting order.
func TestMatcher_SelfTradeCancelNewest(t *testing.T) {
	m := NewMatcher(NewOrderBook("APX"))
	bid, _ := NewOrder("B1", "", "A", "APX", SideBuy, TypeLimit, 100, 5)
	if _, err := m.ProcessOrder(bid); err != nil {
		t.Fatal(err)
	}
	ask, _ := NewOrder("S1", "", "A", "APX", SideSell, TypeLimit, 100, 5)
	ev, err := m.ProcessOrder(ask)
	if err != nil || len(ev) != 0 {
		t.Fatalf("events=%v err=%v", ev, err)
	}
	if ask.Status != StatusRejected {
		t.Errorf("status = %s", ask.Status)
	}
	if _, ok := m.Get("B1"); !ok {
		t.Error("maker removed")
	}
	if _, ok := m.Get("S1"); ok {
		t.Error("rejected taker rested")
	}
}
