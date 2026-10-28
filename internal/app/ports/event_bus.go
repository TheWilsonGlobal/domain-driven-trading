package ports

import (
	"context"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

const (
	TopicOrderPlacement = "orders.placement"
	TopicOrderEvents    = "orders.events"
	TopicMarketDepth    = "market.depth"
	// TopicOrderEventsDLQ receives events a consumer cannot parse or that fail
	// validation; they are acknowledged on the source topic.
	TopicOrderEventsDLQ = "orders.events.dlq"
)

type EventPublisher interface {
	PublishOrderPlaced(ctx context.Context, event order.OrderPlacedEvent) error
	PublishOrderMatched(ctx context.Context, event order.OrderMatchedEvent) error
	PublishOrderCanceled(ctx context.Context, event order.OrderCanceledEvent) error
	PublishMarketDepth(ctx context.Context, event order.MarketDepthEvent) error
	Close() error
}
