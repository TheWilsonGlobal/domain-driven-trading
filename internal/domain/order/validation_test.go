package order

import (
	"errors"
	"testing"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
)

func depthCounts(m *Matcher) (int, int) {
	d := m.Depth(50)
	return len(d.Bids), len(d.Asks)
}

// Plan 2.5: a duplicate order id must be rejected with ErrOrderAlreadyExists
// and must not alter the book (no fills either).
func TestMatcher_DuplicateOrderIDRejected(t *testing.T) {
	m := NewMatcher(NewOrderBook("APX"))
	first, _ := NewOrder("DUP", "", "A", "APX", SideBuy, TypeLimit, 19500, 100)
	if _, _, err := m.Place(first); err != nil {
		t.Fatal(err)
	}
	b0, a0 := depthCounts(m)

	dup, _ := NewOrder("DUP", "", "B", "APX", SideSell, TypeLimit, 19500, 100) // would fully match
	_, events, err := m.Place(dup)
	if !errors.Is(err, common.ErrOrderAlreadyExists) {
		t.Fatalf("err = %v, want ErrOrderAlreadyExists", err)
	}
	if len(events) != 0 {
		t.Errorf("duplicate produced %d fills", len(events))
	}
	if b1, a1 := depthCounts(m); b1 != b0 || a1 != a0 {
		t.Error("book changed")
	}
	if o, _ := m.Get("DUP"); o.FilledQuantity != 0 {
		t.Errorf("resting order was filled: %d", o.FilledQuantity)
	}
}

// Plan 2.4: AddLimitOrder / Place error is propagated, nothing is swallowed.
func TestMatcher_PlaceWrongSymbolReturnsError(t *testing.T) {
	m := NewMatcher(NewOrderBook("APX"))
	resting, _ := NewOrder("R1", "", "A", "APX", SideSell, TypeLimit, 19500, 10)
	_, _, _ = m.Place(resting)
	o, _ := NewOrder("Z1", "", "B", "ZZZ", SideBuy, TypeLimit, 19500, 10)
	if _, events, err := m.Place(o); !errors.Is(err, common.ErrSymbolMismatch) || len(events) != 0 {
		t.Fatalf("err=%v events=%d", err, len(events))
	}
	if _, err := m.ProcessOrder(o); !errors.Is(err, common.ErrSymbolMismatch) {
		t.Fatalf("ProcessOrder err=%v", err)
	}
	if b, a := depthCounts(m); b != 0 || a != 1 {
		t.Errorf("book changed: %d/%d", b, a)
	}
}

func TestPriceTickAndSymbolValidity(t *testing.T) {
	if !Price(19500).IsValidTick() || Price(19505).IsValidTick() {
		t.Error("tick validity wrong")
	}
	for s, want := range map[Symbol]bool{"APX": true, "A1": true, "": false, "apx": false, "A B": false, "ABCDEFGHIJK": false} {
		if got := s.IsValid(); got != want {
			t.Errorf("Symbol(%q).IsValid() = %v, want %v", s, got, want)
		}
	}
}
