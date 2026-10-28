package kafka_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/kafka"
)

// fakePub fails the first failFirst Publish calls (or always when failFirst<0).
type fakePub struct {
	mu        sync.Mutex
	failFirst int
	calls     int
	got       []string
	block     chan struct{}
}

func (f *fakePub) Publish(topic string, msgs ...*message.Message) error {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.failFirst < 0 || f.calls <= f.failFirst {
		return errors.New("broker down")
	}
	for _, m := range msgs {
		f.got = append(f.got, topic+"/"+m.UUID)
	}
	return nil
}
func (f *fakePub) Close() error { return nil }
func (f *fakePub) snapshot() (int, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, append([]string(nil), f.got...)
}

func eventually(t *testing.T, d time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func matched(id string) order.OrderMatchedEvent {
	return order.OrderMatchedEvent{TradeID: id, Symbol: "APX", Price: 19500, Quantity: 1}
}

func fastPolicy() kafka.RetryPolicy {
	return kafka.RetryPolicy{Attempts: 4, BaseDelay: 2 * time.Millisecond, MaxDelay: 10 * time.Millisecond}
}

func TestPublisher_RetriesThenDelivers(t *testing.T) {
	f := &fakePub{failFirst: 2}
	p := kafka.NewWatermillPublisher(f, quiet()).WithRetry(fastPolicy())
	defer p.Close()

	if err := p.PublishOrderMatched(context.Background(), matched("t1")); err != nil {
		t.Fatal(err)
	}
	eventually(t, 2*time.Second, func() bool { _, got := f.snapshot(); return len(got) == 1 }, "delivery after retries")
	if calls, _ := f.snapshot(); calls != 3 {
		t.Fatalf("calls = %d, want 3 (2 failures + 1 success)", calls)
	}
	if n := len(p.DeadLetters()); n != 0 {
		t.Fatalf("dead letters = %d, want 0", n)
	}
}

func TestPublisher_ExhaustedRetriesGoToDeadLetterLog(t *testing.T) {
	f := &fakePub{failFirst: -1}
	p := kafka.NewWatermillPublisher(f, quiet()).WithRetry(fastPolicy())
	defer p.Close()

	if err := p.PublishOrderMatched(context.Background(), matched("t1")); err != nil {
		t.Fatalf("enqueue must not fail: %v", err)
	}
	eventually(t, 2*time.Second, func() bool { return len(p.DeadLetters()) == 1 }, "dead letter")
	dl := p.DeadLetters()[0]
	if dl.Topic != "orders.events" || dl.EventType != "OrderMatched" || len(dl.Payload) == 0 || dl.Err == "" {
		t.Fatalf("dead letter incomplete: %+v", dl)
	}
	if calls, _ := f.snapshot(); calls != 4 {
		t.Fatalf("attempts = %d, want 4", calls)
	}
}

func TestPublisher_NeverBlocksTheCaller(t *testing.T) {
	f := &fakePub{block: make(chan struct{})} // Publish hangs
	p := kafka.NewWatermillPublisher(f, quiet()).WithRetry(fastPolicy()).WithQueueSize(2)
	defer func() { close(f.block); p.Close() }()

	start := time.Now()
	var failed int32
	for i := 0; i < 10; i++ {
		if err := p.PublishOrderMatched(context.Background(), matched("t")); err != nil {
			atomic.AddInt32(&failed, 1)
		}
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("publishing blocked the caller for %v", d)
	}
	if atomic.LoadInt32(&failed) == 0 {
		t.Fatal("overflow of the bounded queue must be reported, not silently dropped")
	}
	if len(p.DeadLetters()) == 0 {
		t.Fatal("overflowed messages must reach the dead-letter log")
	}
}

func TestPublisher_DeadLetterLogIsBounded(t *testing.T) {
	f := &fakePub{failFirst: -1}
	p := kafka.NewWatermillPublisher(f, quiet()).
		WithRetry(kafka.RetryPolicy{Attempts: 1, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}).
		WithDeadLetterLimit(3)
	defer p.Close()
	for i := 0; i < 8; i++ {
		_ = p.PublishOrderMatched(context.Background(), matched("t"))
	}
	eventually(t, 2*time.Second, func() bool { return p.DeadLetterTotal() == 8 }, "8 dead letters counted")
	if n := len(p.DeadLetters()); n != 3 {
		t.Fatalf("retained %d, want the bound 3", n)
	}
}

func TestPublisher_CloseDrainsQueue(t *testing.T) {
	f := &fakePub{}
	p := kafka.NewWatermillPublisher(f, quiet()).WithRetry(fastPolicy())
	for i := 0; i < 20; i++ {
		_ = p.PublishOrderMatched(context.Background(), matched("t"))
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if _, got := f.snapshot(); len(got) != 20 {
		t.Fatalf("delivered %d after Close, want 20", len(got))
	}
	if err := p.PublishOrderMatched(context.Background(), matched("late")); err == nil {
		t.Fatal("publish after Close must fail")
	}
}
