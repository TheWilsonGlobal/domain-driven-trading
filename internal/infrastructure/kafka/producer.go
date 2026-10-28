package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
	"github.com/google/uuid"
)

// ErrPublisherClosed is returned for events offered after Close.
var ErrPublisherClosed = errors.New("event publisher is closed")

// ErrQueueFull is returned when the bounded publish queue overflows: the
// broker has been slower than the engine for longer than the queue can absorb.
var ErrQueueFull = errors.New("event publish queue is full")

// RetryPolicy bounds the retries of one event: Attempts tries in total with
// exponential backoff from BaseDelay up to MaxDelay.
type RetryPolicy struct {
	Attempts  int
	BaseDelay time.Duration
	MaxDelay  time.Duration
}

// DefaultRetryPolicy gives a broker about 3 seconds to come back per event.
var DefaultRetryPolicy = RetryPolicy{Attempts: 5, BaseDelay: 100 * time.Millisecond, MaxDelay: 1500 * time.Millisecond}

// DeadLetter is an event that could not be published (retries exhausted, or
// the queue overflowed). It stays in the bounded in-memory log AND is logged
// at ERROR with its payload size, so it can be re-driven by an operator.
type DeadLetter struct {
	Topic       string
	MessageUUID string
	EventType   string
	Payload     []byte
	Err         string
	At          time.Time
}

type outbound struct {
	topic string
	msg   *message.Message
}

// WatermillPublisher implements ports.EventPublisher on any watermill
// publisher (Kafka in production, gochannel for BUS=memory and tests).
//
// Publishing is asynchronous and never blocks the caller (the order engine's
// request path): events go onto a bounded queue and one worker publishes them
// in order, retrying with backoff (RetryPolicy). When retries are exhausted,
// or the queue is full, the event is written to the dead-letter log (bounded,
// in memory, newest kept) and logged. LIMIT: this outbox is NOT durable; a
// process crash loses queued and dead-lettered events. Trades themselves are
// already persisted by the gateway in the same transaction as the journal, so
// what is lost is the notification, not the ledger.
type WatermillPublisher struct {
	publisher message.Publisher
	logger    *slog.Logger
	retry     RetryPolicy

	once   sync.Once
	mu     sync.RWMutex // guards closed + the queue channel send/close
	closed bool
	queue  chan outbound
	done   chan struct{}
	stop   chan struct{} // closed when Close gives up waiting for the drain

	dlMu    sync.Mutex
	dead    []DeadLetter
	deadMax int
	deadAll int
}

const (
	defaultQueueSize  = 4096
	defaultDeadLetter = 1000
	closeDrainTimeout = 5 * time.Second
)

func NewWatermillPublisher(pub message.Publisher, logger *slog.Logger) *WatermillPublisher {
	p := &WatermillPublisher{
		publisher: pub,
		logger:    logger,
		retry:     DefaultRetryPolicy,
		queue:     make(chan outbound, defaultQueueSize),
		done:      make(chan struct{}),
		stop:      make(chan struct{}),
		deadMax:   defaultDeadLetter,
	}
	return p
}

// WithRetry replaces the retry policy. Call before the first publish.
func (p *WatermillPublisher) WithRetry(r RetryPolicy) *WatermillPublisher {
	if r.Attempts < 1 {
		r.Attempts = 1
	}
	p.retry = r
	return p
}

// WithQueueSize replaces the queue bound. Call before the first publish.
func (p *WatermillPublisher) WithQueueSize(n int) *WatermillPublisher {
	if n < 1 {
		n = 1
	}
	p.queue = make(chan outbound, n)
	return p
}

// WithDeadLetterLimit bounds the dead-letter log. Call before the first publish.
func (p *WatermillPublisher) WithDeadLetterLimit(n int) *WatermillPublisher {
	if n < 1 {
		n = 1
	}
	p.deadMax = n
	return p
}

func (p *WatermillPublisher) ensureWorker() {
	p.once.Do(func() {
		go p.run()
	})
}

func (p *WatermillPublisher) enqueue(topic string, msg *message.Message) error {
	if p.publisher == nil {
		return nil
	}
	p.ensureWorker()
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return ErrPublisherClosed
	}
	select {
	case p.queue <- outbound{topic: topic, msg: msg}:
		return nil
	default:
		p.deadLetter(outbound{topic: topic, msg: msg}, ErrQueueFull)
		return ErrQueueFull
	}
}

func (p *WatermillPublisher) run() {
	defer close(p.done)
	for ob := range p.queue {
		p.deliver(ob)
	}
}

// deliver publishes one event with bounded retries, then dead-letters it.
func (p *WatermillPublisher) deliver(ob outbound) {
	delay := p.retry.BaseDelay
	var err error
	for attempt := 1; attempt <= p.retry.Attempts; attempt++ {
		if err = p.publisher.Publish(ob.topic, ob.msg); err == nil {
			return
		}
		p.logger.Warn("event publish failed", "topic", ob.topic, "attempt", attempt, "of", p.retry.Attempts, "error", err)
		if attempt == p.retry.Attempts {
			break
		}
		select {
		case <-time.After(delay):
		case <-p.stop:
			p.deadLetter(ob, fmt.Errorf("shutdown before retry: %w", err))
			return
		}
		if delay *= 2; delay > p.retry.MaxDelay {
			delay = p.retry.MaxDelay
		}
	}
	p.deadLetter(ob, err)
}

func (p *WatermillPublisher) deadLetter(ob outbound, cause error) {
	dl := DeadLetter{
		Topic:       ob.topic,
		MessageUUID: ob.msg.UUID,
		EventType:   ob.msg.Metadata.Get("event_type"),
		Payload:     ob.msg.Payload,
		Err:         cause.Error(),
		At:          time.Now(),
	}
	p.dlMu.Lock()
	p.deadAll++
	p.dead = append(p.dead, dl)
	if len(p.dead) > p.deadMax {
		p.dead = append([]DeadLetter(nil), p.dead[len(p.dead)-p.deadMax:]...)
	}
	p.dlMu.Unlock()
	p.logger.Error("event dead-lettered: publish gave up",
		"topic", dl.Topic, "event_type", dl.EventType, "message_uuid", dl.MessageUUID,
		"payload_bytes", len(dl.Payload), "error", dl.Err)
}

// DeadLetters returns a copy of the retained dead-letter log (newest last).
func (p *WatermillPublisher) DeadLetters() []DeadLetter {
	p.dlMu.Lock()
	defer p.dlMu.Unlock()
	return append([]DeadLetter(nil), p.dead...)
}

// DeadLetterTotal counts every event ever dead-lettered, including those
// already dropped from the bounded log.
func (p *WatermillPublisher) DeadLetterTotal() int {
	p.dlMu.Lock()
	defer p.dlMu.Unlock()
	return p.deadAll
}

func (p *WatermillPublisher) publish(topic, eventType, id, partitionKey string, v interface{}) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	msg := message.NewMessage(id, payload)
	msg.Metadata.Set("partition_key", partitionKey)
	msg.Metadata.Set("event_type", eventType)
	return p.enqueue(topic, msg)
}

func (p *WatermillPublisher) PublishOrderPlaced(_ context.Context, event order.OrderPlacedEvent) error {
	return p.publish(ports.TopicOrderEvents, "OrderPlaced", uuid.New().String(), string(event.Symbol), event)
}

func (p *WatermillPublisher) PublishOrderMatched(_ context.Context, event order.OrderMatchedEvent) error {
	return p.publish(ports.TopicOrderEvents, "OrderMatched", event.TradeID, string(event.Symbol), event)
}

func (p *WatermillPublisher) PublishOrderCanceled(_ context.Context, event order.OrderCanceledEvent) error {
	return p.publish(ports.TopicOrderEvents, "OrderCanceled", uuid.New().String(), string(event.Symbol), event)
}

func (p *WatermillPublisher) PublishMarketDepth(_ context.Context, event order.MarketDepthEvent) error {
	return p.publish(ports.TopicMarketDepth, "MarketDepth", fmt.Sprintf("%s-%d", event.Symbol, event.Sequence), string(event.Symbol), event)
}

// Close stops accepting events, drains the queue (up to a few seconds; what is
// left is dead-lettered) and closes the underlying publisher.
func (p *WatermillPublisher) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	if p.publisher == nil {
		return nil
	}
	p.ensureWorker()
	close(p.queue)
	select {
	case <-p.done:
	case <-time.After(closeDrainTimeout):
		close(p.stop)
		<-p.done
	}
	return p.publisher.Close()
}

// NewInMemoryPublisher is the BUS=memory / test implementation: a gochannel
// pub/sub. The caller decides (and logs) that memory is wanted.
func NewInMemoryPublisher() (*WatermillPublisher, *gochannel.GoChannel) {
	pubSub := gochannel.NewGoChannel(
		gochannel.Config{OutputChannelBuffer: 1000},
		watermill.NopLogger{},
	)
	return NewWatermillPublisher(pubSub, slog.Default()), pubSub
}
