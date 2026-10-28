package order

import (
	"fmt"
	"sync"
	"testing"
)

// TestMatcher_ConcurrentPlaceCancelDepth hammers the matcher from many goroutines.
// Run with -race: before the locking fix this crashed with a data race / fatal map error.
func TestMatcher_ConcurrentPlaceCancelDepth(t *testing.T) {
	symbol := Symbol("APX")
	m := NewMatcher(NewOrderBook(symbol))

	const workers = 8
	const perWorker = 200

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				side := SideBuy
				if (w+i)%2 == 0 {
					side = SideSell
				}
				price := Price(19500 + int64(i%5)*10)
				o, err := NewOrder(fmt.Sprintf("ORD-%d-%d", w, i), "", "ACC", symbol, side, TypeLimit, price, 10)
				if err != nil {
					t.Errorf("new order: %v", err)
					return
				}
				if _, err := m.ProcessOrder(o); err != nil {
					t.Errorf("process: %v", err)
					return
				}
				if i%3 == 0 {
					_, _ = m.Cancel(o.ID)
				}
				m.Depth(10)
				m.Get(o.ID)
				m.BestBid()
				m.BestAsk()
			}
		}(w)
	}
	wg.Wait()
}

func TestMatcher_PlaceReturnsDetachedSnapshot(t *testing.T) {
	symbol := Symbol("APX")
	m := NewMatcher(NewOrderBook(symbol))
	o, _ := NewOrder("ORD-S1", "", "ACC", symbol, SideBuy, TypeLimit, 19500, 100)
	snap, _, err := m.Place(o)
	if err != nil {
		t.Fatal(err)
	}
	if snap == o {
		t.Fatal("Place returned the live order pointer")
	}
	// A later fill of the resting order must not change the snapshot.
	taker, _ := NewOrder("ORD-S2", "", "ACC2", symbol, SideSell, TypeLimit, 19500, 40)
	if _, err := m.ProcessOrder(taker); err != nil {
		t.Fatal(err)
	}
	if snap.FilledQuantity != 0 {
		t.Errorf("snapshot mutated after the fact: filled=%d", snap.FilledQuantity)
	}
	got, ok := m.Get("ORD-S1")
	if !ok || got.FilledQuantity != 40 || got == o {
		t.Errorf("Get returned %+v ok=%v", got, ok)
	}
	canceled, err := m.Cancel("ORD-S1")
	if err != nil || canceled == o || canceled.Status != StatusCanceled {
		t.Errorf("Cancel: %+v %v", canceled, err)
	}
}
