package kafka_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/kafka"
)

// fakeSub hands out a channel the test feeds by hand.
type fakeSub struct{ ch chan *message.Message }

func (f *fakeSub) Subscribe(context.Context, string) (<-chan *message.Message, error) {
	return f.ch, nil
}
func (f *fakeSub) Close() error { return nil }

// fakeRepo records SaveBatch calls; it fails while failing is true.
type fakeRepo struct {
	mu       sync.Mutex
	failing  bool
	calls    int
	saved    []order.OrderMatchedEvent
	ctxErrs  []error
	batchLen []int
}

func (r *fakeRepo) SaveBatch(ctx context.Context, trades []order.OrderMatchedEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.ctxErrs = append(r.ctxErrs, ctx.Err())
	r.batchLen = append(r.batchLen, len(trades))
	if r.failing {
		return errors.New("db down")
	}
	r.saved = append(r.saved, trades...)
	return nil
}
func (r *fakeRepo) setFailing(v bool) { r.mu.Lock(); r.failing = v; r.mu.Unlock() }
func (r *fakeRepo) numCalls() int     { r.mu.Lock(); defer r.mu.Unlock(); return r.calls }
func (r *fakeRepo) numSaved() int     { r.mu.Lock(); defer r.mu.Unlock(); return len(r.saved) }

// capturePub records dead-letter publications.
type capturePub struct {
	mu   sync.Mutex
	msgs map[string][]*message.Message
}

func (c *capturePub) Publish(topic string, msgs ...*message.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.msgs == nil {
		c.msgs = map[string][]*message.Message{}
	}
	c.msgs[topic] = append(c.msgs[topic], msgs...)
	return nil
}
func (c *capturePub) Close() error { return nil }
func (c *capturePub) count(topic string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.msgs[topic])
}

func goodEvent(id string) order.OrderMatchedEvent {
	return order.OrderMatchedEvent{
		TradeID: id, Symbol: "APX", MakerOrderID: "m1", TakerOrderID: "t1",
		MakerAccountID: "MAKER", TakerAccountID: "TAKER", TakerSide: order.SideBuy,
		Price: 19500, Quantity: 10, Timestamp: time.Now(),
	}
}

func matchedMsg(t *testing.T, ev order.OrderMatchedEvent) *message.Message {
	t.Helper()
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	m := message.NewMessage(ev.TradeID, b)
	m.Metadata.Set("event_type", "OrderMatched")
	return m
}

func isAcked(m *message.Message) bool {
	select {
	case <-m.Acked():
		return true
	default:
		return false
	}
}
func isNacked(m *message.Message) bool {
	select {
	case <-m.Nacked():
		return true
	default:
		return false
	}
}

type harness struct {
	c      *kafka.SettlementConsumer
	sub    *fakeSub
	repo   *fakeRepo
	dlq    *capturePub
	cancel context.CancelFunc
	done   chan error
}

func startConsumer(t *testing.T, flush time.Duration, attempts int, preload ...*message.Message) *harness {
	t.Helper()
	h := &harness{
		sub:  &fakeSub{ch: make(chan *message.Message, 100)},
		repo: &fakeRepo{},
		dlq:  &capturePub{},
		done: make(chan error, 1),
	}
	for _, m := range preload {
		h.sub.ch <- m
	}
	h.c = kafka.NewSettlementConsumer(h.sub, h.repo, quiet()).
		WithDeadLetter(h.dlq).
		WithBatching(50, flush).
		WithMaxFlushAttempts(attempts)
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() { h.done <- h.c.Start(ctx) }()
	t.Cleanup(func() { cancel() })
	return h
}

func (h *harness) stop(t *testing.T) {
	t.Helper()
	h.cancel()
	select {
	case <-h.done:
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not stop")
	}
}

func TestConsumer_AcksOnlyAfterSaveBatchSucceeds(t *testing.T) {
	m1, m2, m3 := matchedMsg(t, goodEvent("a")), matchedMsg(t, goodEvent("b")), matchedMsg(t, goodEvent("c"))
	h := startConsumer(t, 20*time.Millisecond, 5, m1, m2, m3)
	eventually(t, 2*time.Second, func() bool { return isAcked(m1) && isAcked(m2) && isAcked(m3) }, "acks")
	if h.repo.numSaved() != 3 {
		t.Fatalf("saved %d, want 3", h.repo.numSaved())
	}
	if isNacked(m1) {
		t.Fatal("must not nack on success")
	}
}

func TestConsumer_FailingSaveBatchKeepsMessagesUnackedAndBatchRetained(t *testing.T) {
	m1, m2 := matchedMsg(t, goodEvent("a")), matchedMsg(t, goodEvent("b"))
	h := startConsumer(t, 15*time.Millisecond, 1000, m1, m2)
	h.repo.setFailing(true)
	eventually(t, 2*time.Second, func() bool { return h.repo.numCalls() >= 2 }, "retry of the failed flush")

	if isAcked(m1) || isAcked(m2) {
		t.Fatal("messages acked although SaveBatch failed")
	}
	if got := h.c.Pending(); got != 2 {
		t.Fatalf("pending batch = %d, want 2 (retained)", got)
	}

	h.repo.setFailing(false) // the database recovers
	eventually(t, 2*time.Second, func() bool { return isAcked(m1) && isAcked(m2) }, "ack after recovery")
	if h.repo.numSaved() != 2 || h.c.Pending() != 0 {
		t.Fatalf("saved=%d pending=%d, want 2 and 0", h.repo.numSaved(), h.c.Pending())
	}
}

func TestConsumer_NacksForRedeliveryAfterRepeatedFailures(t *testing.T) {
	m1 := matchedMsg(t, goodEvent("a"))
	h := startConsumer(t, 10*time.Millisecond, 3, m1)
	h.repo.setFailing(true)
	eventually(t, 2*time.Second, func() bool { return isNacked(m1) }, "nack after max attempts")
	if isAcked(m1) {
		t.Fatal("must not ack a message that was never persisted")
	}
	if h.c.Pending() != 0 {
		t.Fatalf("pending = %d after nack, want 0 (broker redelivers)", h.c.Pending())
	}
}

func TestConsumer_ShutdownFlushUsesFreshContext(t *testing.T) {
	m1 := matchedMsg(t, goodEvent("a"))
	h := startConsumer(t, time.Hour, 1000, m1) // no ticker help: only the shutdown flush can save it
	h.repo.setFailing(true)
	eventually(t, 2*time.Second, func() bool { return h.repo.numCalls() >= 1 }, "first failed flush")
	h.repo.setFailing(false)

	h.stop(t) // cancels the consumer context
	if h.repo.numSaved() != 1 {
		t.Fatalf("shutdown flush saved %d, want 1", h.repo.numSaved())
	}
	if !isAcked(m1) {
		t.Fatal("message not acked after the shutdown flush")
	}
	h.repo.mu.Lock()
	last := h.repo.ctxErrs[len(h.repo.ctxErrs)-1]
	h.repo.mu.Unlock()
	if last != nil {
		t.Fatalf("shutdown SaveBatch ran with a cancelled context: %v", last)
	}
}

func TestConsumer_ShutdownFlushFailureNacks(t *testing.T) {
	m1 := matchedMsg(t, goodEvent("a"))
	h := startConsumer(t, time.Hour, 1000, m1)
	h.repo.setFailing(true)
	eventually(t, 2*time.Second, func() bool { return h.repo.numCalls() >= 1 }, "first failed flush")
	h.stop(t)
	if !isNacked(m1) || isAcked(m1) {
		t.Fatal("an unpersisted message must be nacked at shutdown, never acked")
	}
}

func TestConsumer_PoisonMessagesAreDeadLetteredAndAcked(t *testing.T) {
	garbage := message.NewMessage("p1", []byte("{not json"))
	garbage.Metadata.Set("event_type", "OrderMatched")
	neg := goodEvent("neg")
	neg.Price = -5
	negMsg := matchedMsg(t, neg)
	good := matchedMsg(t, goodEvent("ok"))

	h := startConsumer(t, 10*time.Millisecond, 5, garbage, negMsg, good)
	eventually(t, 2*time.Second, func() bool { return isAcked(garbage) && isAcked(negMsg) && isAcked(good) }, "all acked")

	if h.dlq.count("orders.events.dlq") != 2 {
		t.Fatalf("dead-lettered %d, want 2", h.dlq.count("orders.events.dlq"))
	}
	if h.repo.numSaved() != 1 {
		t.Fatalf("saved %d, want only the valid event", h.repo.numSaved())
	}
	h.dlq.mu.Lock()
	reason := h.dlq.msgs["orders.events.dlq"][0].Metadata.Get("dead_letter_reason")
	h.dlq.mu.Unlock()
	if reason == "" {
		t.Fatal("dead-letter message carries no reason")
	}
}

func TestConsumer_OtherEventTypesAreAckedWithoutPersisting(t *testing.T) {
	m := message.NewMessage("x", []byte(`{}`))
	m.Metadata.Set("event_type", "OrderPlaced")
	h := startConsumer(t, 10*time.Millisecond, 5, m)
	eventually(t, 2*time.Second, func() bool { return isAcked(m) }, "ack")
	if h.repo.numCalls() != 0 {
		t.Fatal("non-trade events must not reach SaveBatch")
	}
}

func TestValidateMatchedEvent(t *testing.T) {
	base := goodEvent("t1")
	mut := func(f func(*order.OrderMatchedEvent)) order.OrderMatchedEvent { e := base; f(&e); return e }
	tests := []struct {
		name string
		ev   order.OrderMatchedEvent
		ok   bool
	}{
		{"valid", base, true},
		{"zero price", mut(func(e *order.OrderMatchedEvent) { e.Price = 0 }), false},
		{"negative price", mut(func(e *order.OrderMatchedEvent) { e.Price = -1 }), false},
		{"price over max", mut(func(e *order.OrderMatchedEvent) { e.Price = order.MaxPrice + 1 }), false},
		{"zero qty", mut(func(e *order.OrderMatchedEvent) { e.Quantity = 0 }), false},
		{"qty over max", mut(func(e *order.OrderMatchedEvent) { e.Quantity = order.MaxQty + 1 }), false},
		{"negative unlock", mut(func(e *order.OrderMatchedEvent) { e.TakerUnlock = -1 }), false},
		{"absurd unlock", mut(func(e *order.OrderMatchedEvent) { e.MakerUnlock = 1 << 62 }), false},
		{"missing trade id", mut(func(e *order.OrderMatchedEvent) { e.TradeID = "" }), false},
		{"bad symbol", mut(func(e *order.OrderMatchedEvent) { e.Symbol = "apx!" }), false},
		{"missing maker order", mut(func(e *order.OrderMatchedEvent) { e.MakerOrderID = "" }), false},
		{"bad taker account", mut(func(e *order.OrderMatchedEvent) { e.TakerAccountID = "a b" }), false},
		{"bad side", mut(func(e *order.OrderMatchedEvent) { e.TakerSide = "HOLD" }), false},
		{"zero timestamp", mut(func(e *order.OrderMatchedEvent) { e.Timestamp = time.Time{} }), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := kafka.ValidateMatchedEvent(tc.ev)
			if tc.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

type failingPub struct{}

func (failingPub) Publish(string, ...*message.Message) error { return errors.New("dlq down") }
func (failingPub) Close() error                              { return nil }

// SEC-22: when the dead-letter publish fails the poison message is Nacked
// (redelivered), not Acked, so it is not lost.
func TestConsumer_PoisonIsNackedWhenDeadLetterPublishFails(t *testing.T) {
	garbage := message.NewMessage("g1", []byte("not json"))
	garbage.Metadata.Set("event_type", "OrderMatched")
	sub := &fakeSub{ch: make(chan *message.Message, 10)}
	sub.ch <- garbage
	c := kafka.NewSettlementConsumer(sub, &fakeRepo{}, quiet()).WithDeadLetter(failingPub{}).WithBatching(50, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Start(ctx) }()
	eventually(t, 2*time.Second, func() bool { return isNacked(garbage) }, "nack on failed DLQ publish")
	if isAcked(garbage) {
		t.Fatal("poison message acked although it was not dead-lettered")
	}
}

// With no DLQ publisher configured an ERROR is logged and the message is Acked
// (otherwise one poison message would be redelivered forever).
func TestConsumer_PoisonWithoutDeadLetterIsAckedAndLoggedAsError(t *testing.T) {
	garbage := message.NewMessage("g2", []byte("not json"))
	garbage.Metadata.Set("event_type", "OrderMatched")
	sub := &fakeSub{ch: make(chan *message.Message, 10)}
	sub.ch <- garbage
	var buf syncBuf
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	c := kafka.NewSettlementConsumer(sub, &fakeRepo{}, logger).WithBatching(50, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Start(ctx) }()
	eventually(t, 2*time.Second, func() bool { return isAcked(garbage) }, "ack without a DLQ")
	if isNacked(garbage) {
		t.Fatal("nacked without a DLQ: infinite redelivery loop")
	}
	if !strings.Contains(buf.String(), "level=ERROR") || !strings.Contains(buf.String(), "no dead-letter publisher") {
		t.Fatalf("expected an ERROR naming the missing dead-letter publisher, got: %s", buf.String())
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }
