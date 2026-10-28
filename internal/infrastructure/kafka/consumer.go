package kafka

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
	"github.com/google/uuid"
)

// SettlementConsumer is the durable trade-persistence worker.
//
// Role next to the gateway: the gateway already writes every execution, with
// its double-entry journal, in one PostgreSQL transaction (TradeJournalRepository).
// This worker consumes the same executions from the bus and idempotently
// inserts them into trades (INSERT ... ON CONFLICT (trade_id) DO NOTHING). It
// therefore tolerates the gateway having written the row first, and it is the
// safety net that recovers a trade row if the gateway's write was lost. It
// never writes the ledger; the journal stays owned by the gateway transaction.
//
// Delivery contract:
//   - a message is Acked only after SaveBatch has succeeded for it;
//   - on failure the batch is retained and retried on the next tick; after
//     maxAttempts consecutive failures the messages are Nacked, so the broker
//     redelivers them (ON CONFLICT makes redelivery harmless);
//   - on shutdown the remaining batch is flushed with a fresh timeout context
//     (the service context is already cancelled) and Nacked if that fails;
//   - payloads that cannot be parsed or fail validation are published to
//     orders.events.dlq, logged, and Acked so one poison message cannot block
//     the partition; if that publish fails the message is Nacked (redelivered,
//     never lost); with NO dead-letter publisher an ERROR is logged and the
//     message is Acked (a Nack would loop forever).
//
// Batching note: watermill delivers one message at a time per partition and
// waits for its Ack before the next, so the batch is "whatever is available
// right now": typically one message per partition, more when several
// partitions deliver concurrently.
type SettlementConsumer struct {
	subscriber message.Subscriber
	tradeRepo  ports.TradeRepository
	logger     *slog.Logger
	dlq        message.Publisher

	batchSize       int
	flushTime       time.Duration
	maxAttempts     int
	shutdownTimeout time.Duration

	mu       sync.Mutex
	pending  []pendingTrade
	attempts int
}

type pendingTrade struct {
	event order.OrderMatchedEvent
	msg   *message.Message
}

func NewSettlementConsumer(
	sub message.Subscriber,
	tradeRepo ports.TradeRepository,
	logger *slog.Logger,
) *SettlementConsumer {
	return &SettlementConsumer{
		subscriber:      sub,
		tradeRepo:       tradeRepo,
		logger:          logger,
		batchSize:       50,
		flushTime:       200 * time.Millisecond,
		maxAttempts:     5,
		shutdownTimeout: 5 * time.Second,
	}
}

// WithDeadLetter sets where poison messages are published.
func (c *SettlementConsumer) WithDeadLetter(p message.Publisher) *SettlementConsumer {
	c.dlq = p
	return c
}

// WithBatching sets the batch bound and the retry tick.
func (c *SettlementConsumer) WithBatching(size int, flush time.Duration) *SettlementConsumer {
	if size > 0 {
		c.batchSize = size
	}
	if flush > 0 {
		c.flushTime = flush
	}
	return c
}

// WithMaxFlushAttempts sets how many consecutive SaveBatch failures are
// retried before the messages are Nacked for redelivery.
func (c *SettlementConsumer) WithMaxFlushAttempts(n int) *SettlementConsumer {
	if n > 0 {
		c.maxAttempts = n
	}
	return c
}

// Pending reports how many accepted messages still wait for a successful save.
func (c *SettlementConsumer) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending)
}

func (c *SettlementConsumer) Start(ctx context.Context) error {
	messages, err := c.subscriber.Subscribe(ctx, ports.TopicOrderEvents)
	if err != nil {
		return err
	}
	c.logger.Info("settlement consumer listening on topic", "topic", ports.TopicOrderEvents)

	ticker := time.NewTicker(c.flushTime)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.shutdownFlush()
			return ctx.Err()

		case <-ticker.C:
			c.flush(ctx)

		case msg, ok := <-messages:
			if !ok {
				c.shutdownFlush()
				return nil
			}
			c.handle(msg)
			closed := false
		drain:
			for c.Pending() < c.batchSize {
				select {
				case m, ok := <-messages:
					if !ok {
						closed = true
						break drain
					}
					c.handle(m)
				default:
					break drain
				}
			}
			c.flush(ctx)
			if closed {
				c.shutdownFlush()
				return nil
			}
		}
	}
}

// handle classifies one message: other event types are acked, poison is
// dead-lettered and acked, a valid trade joins the pending batch un-acked.
func (c *SettlementConsumer) handle(msg *message.Message) {
	if msg.Metadata.Get("event_type") != "OrderMatched" {
		msg.Ack()
		return
	}
	var ev order.OrderMatchedEvent
	if err := json.Unmarshal(msg.Payload, &ev); err != nil {
		c.deadLetter(msg, "unparseable payload: "+err.Error())
		return
	}
	if err := ValidateMatchedEvent(ev); err != nil {
		c.deadLetter(msg, "invalid event: "+err.Error())
		return
	}
	c.mu.Lock()
	c.pending = append(c.pending, pendingTrade{event: ev, msg: msg})
	c.mu.Unlock()
}

func (c *SettlementConsumer) deadLetter(msg *message.Message, reason string) {
	c.logger.Error("dead-lettering unprocessable settlement event",
		"message_uuid", msg.UUID, "reason", reason, "payload_bytes", len(msg.Payload))
	if c.dlq == nil {
		// Nacking would redeliver the same poison message forever.
		c.logger.Error("no dead-letter publisher configured; poison event dropped (acked) and only in this log", "message_uuid", msg.UUID)
		msg.Ack()
		return
	}
	dl := message.NewMessage(uuid.New().String(), msg.Payload)
	for k, v := range msg.Metadata {
		dl.Metadata.Set(k, v)
	}
	dl.Metadata.Set("dead_letter_reason", reason)
	dl.Metadata.Set("source_topic", ports.TopicOrderEvents)
	dl.Metadata.Set("source_message_uuid", msg.UUID)
	if err := c.dlq.Publish(ports.TopicOrderEventsDLQ, dl); err != nil {
		// The event would be lost: Nack so the broker redelivers it.
		c.logger.Error("dead-letter publish failed; nacking for redelivery", "message_uuid", msg.UUID, "error", err)
		msg.Nack()
		return
	}
	msg.Ack()
}

// flush saves the pending batch. Success acks it; failure keeps it (and its
// un-acked messages) for the next tick until maxAttempts, then nacks it.
func (c *SettlementConsumer) flush(ctx context.Context) {
	c.save(ctx, false)
}

// shutdownFlush persists what is left with a fresh context: the service
// context is already cancelled, so reusing it would fail every write.
func (c *SettlementConsumer) shutdownFlush() {
	ctx, cancel := context.WithTimeout(context.Background(), c.shutdownTimeout)
	defer cancel()
	c.save(ctx, true)
}

func (c *SettlementConsumer) save(ctx context.Context, final bool) {
	c.mu.Lock()
	if len(c.pending) == 0 {
		c.mu.Unlock()
		return
	}
	batch := make([]pendingTrade, len(c.pending))
	copy(batch, c.pending)
	c.mu.Unlock()

	events := make([]order.OrderMatchedEvent, len(batch))
	for i, p := range batch {
		events[i] = p.event
	}

	if err := c.tradeRepo.SaveBatch(ctx, events); err != nil {
		c.mu.Lock()
		c.attempts++
		giveUp := final || c.attempts >= c.maxAttempts
		if giveUp {
			c.pending = c.pending[len(batch):]
			c.attempts = 0
		}
		attempts := c.attempts
		c.mu.Unlock()

		if giveUp {
			c.logger.Error("failed to persist trade batch; nacking for redelivery", "error", err, "count", len(batch))
			for _, p := range batch {
				p.msg.Nack()
			}
			return
		}
		c.logger.Error("failed to persist trade batch; keeping it for retry", "error", err, "count", len(batch), "attempt", attempts)
		return
	}

	c.mu.Lock()
	c.pending = c.pending[len(batch):]
	c.attempts = 0
	c.mu.Unlock()
	for _, p := range batch {
		p.msg.Ack()
	}
	c.logger.Info("persisted trade batch", "count", len(batch))
}
