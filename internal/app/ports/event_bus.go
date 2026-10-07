package ports

import (
	"context"

	"github.com/vortex-trading/vortex-trading-engine/internal/domain/order"
)

const (
	TopicOrderPlacement = "orders.placement"
	TopicOrderEvents    = "orders.events"
	TopicMarketDepth    = "market.depth"
)

type EventPublisher interface {
	PublishOrderPlaced(ctx context.Context, event order.OrderPlacedEvent) error
	PublishOrderMatched(ctx context.Context, event order.OrderMatchedEvent) error
	PublishOrderCanceled(ctx context.Context, event order.OrderCanceledEvent) error
	PublishMarketDepth(ctx context.Context, event order.MarketDepthEvent) error
	Close() error
}
