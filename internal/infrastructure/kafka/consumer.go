package kafka

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/vortex-trading/vortex-trading-engine/internal/app/ports"
	"github.com/vortex-trading/vortex-trading-engine/internal/domain/order"
)

type SettlementConsumer struct {
	subscriber message.Subscriber
	tradeRepo  ports.TradeRepository
	logger     *slog.Logger
	batchSize  int
	flushTime  time.Duration
}

func NewSettlementConsumer(
	sub message.Subscriber,
	tradeRepo ports.TradeRepository,
	logger *slog.Logger,
) *SettlementConsumer {
	return &SettlementConsumer{
		subscriber: sub,
		tradeRepo:  tradeRepo,
		logger:     logger,
		batchSize:  50,
		flushTime:  200 * time.Millisecond,
	}
}

func (c *SettlementConsumer) Start(ctx context.Context) error {
	messages, err := c.subscriber.Subscribe(ctx, ports.TopicOrderEvents)
	if err != nil {
		return err
	}

	c.logger.Info("settlement consumer listening on topic", "topic", ports.TopicOrderEvents)

	var mu sync.Mutex
	tradeBatch := make([]order.OrderMatchedEvent, 0, c.batchSize)
	ticker := time.NewTicker(c.flushTime)
	defer ticker.Stop()

	flush := func() {
		mu.Lock()
		if len(tradeBatch) == 0 {
			mu.Unlock()
			return
		}
		toPersist := make([]order.OrderMatchedEvent, len(tradeBatch))
		copy(toPersist, tradeBatch)
		tradeBatch = tradeBatch[:0]
		mu.Unlock()

		if err := c.tradeRepo.SaveBatch(ctx, toPersist); err != nil {
			c.logger.Error("failed to batch insert trades into PostgreSQL", "error", err, "count", len(toPersist))
		} else {
			c.logger.Info("persisted trade batch into PostgreSQL", "count", len(toPersist))
		}
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return ctx.Err()

		case <-ticker.C:
			flush()

		case msg, ok := <-messages:
			if !ok {
				flush()
				return nil
			}

			eventType := msg.Metadata.Get("event_type")
			if eventType == "OrderMatched" {
				var matchEvent order.OrderMatchedEvent
				if err := json.Unmarshal(msg.Payload, &matchEvent); err == nil {
					mu.Lock()
					tradeBatch = append(tradeBatch, matchEvent)
					shouldFlushNow := len(tradeBatch) >= c.batchSize
					mu.Unlock()

					if shouldFlushNow {
						flush()
					}
				}
			}
			msg.Ack()
		}
	}
}
