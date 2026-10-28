package memory_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/persistence/memory"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

var _ ports.IdempotencyStore = (*memory.IdempotencyStore)(nil)

func TestIdempotencyStore_ClaimCompleteReplayRelease(t *testing.T) {
	ctx := context.Background()
	s := memory.NewIdempotencyStore(100)

	ok, _, err := s.Claim(ctx, "k", "fp", time.Minute)
	if err != nil || !ok {
		t.Fatalf("first claim = %v, %v", ok, err)
	}
	ok, rec, _ := s.Claim(ctx, "k", "fp", time.Minute)
	if ok || rec.Done {
		t.Fatalf("second claim must report an in-flight owner, got ok=%v rec=%+v", ok, rec)
	}
	if err := s.Complete(ctx, "k", ports.IdempotencyRecord{Fingerprint: "fp", Status: 201, Body: []byte(`{"a":1}`)}, time.Hour); err != nil {
		t.Fatal(err)
	}
	ok, rec, _ = s.Claim(ctx, "k", "fp", time.Minute)
	if ok || !rec.Done || rec.Status != 201 || string(rec.Body) != `{"a":1}` {
		t.Fatalf("replay = ok=%v rec=%+v", ok, rec)
	}

	if _, _, err := s.Claim(ctx, "k2", "fp", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := s.Release(ctx, "k2"); err != nil {
		t.Fatal(err)
	}
	if ok, _, _ := s.Claim(ctx, "k2", "fp", time.Minute); !ok {
		t.Fatal("a released key must be claimable again")
	}
}

func TestIdempotencyStore_ExactlyOneClaimWins(t *testing.T) {
	s := memory.NewIdempotencyStore(100)
	var wins int32
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _, _ := s.Claim(context.Background(), "k", "fp", time.Minute); ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d claims won, want exactly 1", wins)
	}
}

func TestIdempotencyStore_IsBoundedByCount(t *testing.T) {
	s := memory.NewIdempotencyStore(10)
	for i := 0; i < 100; i++ {
		if _, _, err := s.Claim(context.Background(), fmt.Sprintf("k%d", i), "fp", time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if n := s.Len(); n > 10 {
		t.Fatalf("store holds %d entries, bound is 10", n)
	}
}

func TestIdempotencyStore_ExpiredEntriesAreEvicted(t *testing.T) {
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := memory.NewIdempotencyStoreWithClock(1000, c.now)
	for i := 0; i < 20; i++ {
		_, _, _ = s.Claim(context.Background(), fmt.Sprintf("k%d", i), "fp", time.Minute)
	}
	c.add(2 * time.Minute)
	if ok, _, _ := s.Claim(context.Background(), "k0", "fp", time.Minute); !ok {
		t.Fatal("an expired key must be claimable again")
	}
	s.Sweep()
	if n := s.Len(); n != 1 {
		t.Fatalf("after TTL only the fresh claim should remain, have %d", n)
	}
}
