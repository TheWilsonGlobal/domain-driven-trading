package order

import (
	"testing"
)

func TestOrderBook_FIFO_PriceTimePriority(t *testing.T) {
	symbol := Symbol("APX")
	ob := NewOrderBook(symbol)
	matcher := NewMatcher(ob)

	// Step 1: Add Maker 1: BUY 100 @ 19,500
	m1, err := NewOrder("ORD-001", "C-001", "ACC-1", symbol, SideBuy, TypeLimit, 19500, 100)
	if err != nil {
		t.Fatalf("failed to create order 1: %v", err)
	}
	_, err = matcher.ProcessOrder(m1)
	if err != nil {
		t.Fatalf("failed to process order 1: %v", err)
	}

	// Step 2: Add Maker 2: BUY 200 @ 19,500 (Same price, but later timestamp)
	m2, err := NewOrder("ORD-002", "C-002", "ACC-2", symbol, SideBuy, TypeLimit, 19500, 200)
	if err != nil {
		t.Fatalf("failed to create order 2: %v", err)
	}
	_, err = matcher.ProcessOrder(m2)
	if err != nil {
		t.Fatalf("failed to process order 2: %v", err)
	}

	// Step 3: Add Maker 3: BUY 150 @ 19,600 (Higher price, should have price priority over 19,500)
	m3, err := NewOrder("ORD-003", "C-003", "ACC-3", symbol, SideBuy, TypeLimit, 19600, 150)
	if err != nil {
		t.Fatalf("failed to create order 3: %v", err)
	}
	_, err = matcher.ProcessOrder(m3)
	if err != nil {
		t.Fatalf("failed to process order 3: %v", err)
	}

	// Incoming Taker SELL 200 @ 19,500
	// Priority rule:
	// 1st: Must match m3 (19,600) for 150 shares (Price priority)
	// 2nd: Remaining 50 shares must match m1 (19,500) because m1 was queued before m2 (Time FIFO priority)
	taker, err := NewOrder("ORD-TAKER", "C-TAKER", "ACC-SELLER", symbol, SideSell, TypeLimit, 19500, 200)
	if err != nil {
		t.Fatalf("failed to create taker: %v", err)
	}

	events, err := matcher.ProcessOrder(taker)
	if err != nil {
		t.Fatalf("matching failed: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 match events, got %d", len(events))
	}

	// 1st match: Maker 3 @ 19,600 for 150
	if events[0].MakerOrderID != "ORD-003" || events[0].Quantity != 150 || events[0].Price != 19600 {
		t.Errorf("expected first match to be ORD-003 @ 19600 for 150 shares, got %+v", events[0])
	}

	// 2nd match: Maker 1 @ 19,500 for 50 (FIFO time priority before Maker 2!)
	if events[1].MakerOrderID != "ORD-001" || events[1].Quantity != 50 || events[1].Price != 19500 {
		t.Errorf("expected second match to be ORD-001 @ 19500 for 50 shares, got %+v", events[1])
	}

	// Verify remaining Maker 1 is 50, and Maker 2 remains untouched (200 shares)
	if m1.RemainingQuantity() != 50 {
		t.Errorf("expected m1 remaining quantity to be 50, got %d", m1.RemainingQuantity())
	}
	if m2.RemainingQuantity() != 200 {
		t.Errorf("expected m2 remaining quantity to be 200, got %d", m2.RemainingQuantity())
	}
}
