package query

import (
	"context"

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

func (h *GetOrderBookHandler) Handle(ctx context.Context, q GetOrderBookQuery) order.MarketDepthEvent {
	levels := q.MaxLevels
	if levels <= 0 {
		levels = 10
	}
	return h.matcher.Book().GetL2Depth(levels)
}
