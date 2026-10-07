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
	bestBidPrice, bidQueue, exists := ob.BestBid()
	if !exists || bestBidPrice != 28000 || bidQueue.TotalQuantity != 100 {
		t.Errorf("expected best bid 28000 with 100 shares, got exists=%v price=%d qty=%d", exists, bestBidPrice, bidQueue.TotalQuantity)
	}
}

func BenchmarkMatcher_LimitOrderMatching(b *testing.B) {
	symbol := Symbol("VPB")
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
