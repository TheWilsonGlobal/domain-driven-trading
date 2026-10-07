package command

import (
	"context"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

type CancelOrderCommand struct {
	OrderID   string
	AccountID string
	Symbol    order.Symbol
}

type CancelOrderHandler struct {
	matcher     *order.Matcher
	accountRepo ports.AccountRepository
	eventBus    ports.EventPublisher
}

func NewCancelOrderHandler(
	matcher *order.Matcher,
	accountRepo ports.AccountRepository,
	eventBus ports.EventPublisher,
) *CancelOrderHandler {
	return &CancelOrderHandler{
		matcher:     matcher,
		accountRepo: accountRepo,
		eventBus:    eventBus,
	}
}

func (h *CancelOrderHandler) Handle(ctx context.Context, cmd CancelOrderCommand) (*order.Order, error) {
	canceledOrder, err := h.matcher.Book().CancelOrder(cmd.OrderID)
	if err != nil {
		return nil, err
	}

	// Release locked funds or stock
	acc, err := h.accountRepo.GetByID(ctx, cmd.AccountID)
	if err == nil && acc != nil {
		if canceledOrder.Side == order.SideBuy {
			releasedFunds := account.CalculateRequiredFunds(
				canceledOrder.Price,
				canceledOrder.RemainingQuantity(),
				account.DefaultFeeRateBasisPoints,
			)
			acc.ReleaseBuyFunds(releasedFunds)
		} else {
			acc.ReleaseSellHolding(canceledOrder.Symbol, canceledOrder.RemainingQuantity())
		}
	}

	if h.eventBus != nil {
		_ = h.eventBus.PublishOrderCanceled(ctx, order.OrderCanceledEvent{
			OrderID:          canceledOrder.ID,
			AccountID:        canceledOrder.AccountID,
			Symbol:           canceledOrder.Symbol,
			CanceledQuantity: canceledOrder.RemainingQuantity(),
			Reason:           "USER_REQUESTED",
			Timestamp:        time.Now(),
		})

		depth := h.matcher.Book().GetL2Depth(10)
		_ = h.eventBus.PublishMarketDepth(ctx, depth)
	}

	return canceledOrder, nil
}
