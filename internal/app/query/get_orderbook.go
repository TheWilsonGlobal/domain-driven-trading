package query

import (
	"context"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

type GetOrderBookQuery struct {
	Symbol    order.Symbol
	MaxLevels int
}

type GetOrderBookHandler struct {
	matcher *order.Matcher
}

func NewGetOrderBookHandler(matcher *order.Matcher) *GetOrderBookHandler {
	return &GetOrderBookHandler{
		matcher: matcher,
	}
}

const (
	DefaultOrderBookLevels = 10
	MaxOrderBookLevels     = 50
)

// Handle returns L2 depth. An empty symbol means the book's own symbol; any
// other symbol than the book's is rejected with ErrSymbolMismatch. Levels
// <= 0 mean the default and values above MaxOrderBookLevels are clamped.
func (h *GetOrderBookHandler) Handle(ctx context.Context, q GetOrderBookQuery) (order.MarketDepthEvent, error) {
	if q.Symbol != "" && q.Symbol != h.matcher.Symbol() {
		return order.MarketDepthEvent{}, common.ErrSymbolMismatch
	}
	levels := q.MaxLevels
	if levels <= 0 {
		levels = DefaultOrderBookLevels
	}
	if levels > MaxOrderBookLevels {
		levels = MaxOrderBookLevels
	}
	return h.matcher.Depth(levels), nil
}
