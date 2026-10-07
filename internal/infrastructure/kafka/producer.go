package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

type WatermillPublisher struct {
	publisher message.Publisher
	logger    *slog.Logger
}

func NewWatermillPublisher(pub message.Publisher, logger *slog.Logger) *WatermillPublisher {
	return &WatermillPublisher{
		publisher: pub,
		logger:    logger,
	}
}

func (p *WatermillPublisher) PublishOrderPlaced(ctx context.Context, event order.OrderPlacedEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	msg := message.NewMessage(uuid.New().String(), payload)
	msg.Metadata.Set("partition_key", string(event.Symbol))
	msg.Metadata.Set("event_type", "OrderPlaced")

	if p.publisher != nil {
		return p.publisher.Publish(ports.TopicOrderEvents, msg)
	}
	return nil
}

func (p *WatermillPublisher) PublishOrderMatched(ctx context.Context, event order.OrderMatchedEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	msg := message.NewMessage(event.TradeID, payload)
	msg.Metadata.Set("partition_key", string(event.Symbol))
	msg.Metadata.Set("event_type", "OrderMatched")

	if p.publisher != nil {
		return p.publisher.Publish(ports.TopicOrderEvents, msg)
	}
	return nil
}

func (p *WatermillPublisher) PublishOrderCanceled(ctx context.Context, event order.OrderCanceledEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	msg := message.NewMessage(uuid.New().String(), payload)
	msg.Metadata.Set("partition_key", string(event.Symbol))
	msg.Metadata.Set("event_type", "OrderCanceled")

	if p.publisher != nil {
		return p.publisher.Publish(ports.TopicOrderEvents, msg)
	}
	return nil
}

func (p *WatermillPublisher) PublishMarketDepth(ctx context.Context, event order.MarketDepthEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	msg := message.NewMessage(fmt.Sprintf("%s-%d", event.Symbol, event.Sequence), payload)
	msg.Metadata.Set("partition_key", string(event.Symbol))
	msg.Metadata.Set("event_type", "MarketDepth")

	if p.publisher != nil {
		return p.publisher.Publish(ports.TopicMarketDepth, msg)
	}
	return nil
}

func (p *WatermillPublisher) Close() error {
	if p.publisher != nil {
		return p.publisher.Close()
	}
	return nil
}

// In-memory PubSub fallback for testing and lightweight standalone setups
func NewInMemoryPublisher() (*WatermillPublisher, *message.PubSub) {
	pubSub := message.NewPubSub(watermill.NewStdLogger(false, false))
	return NewWatermillPublisher(pubSub, slog.Default()), pubSub
}
