package command

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/vortex-trading/vortex-trading-engine/internal/app/ports"
	"github.com/vortex-trading/vortex-trading-engine/internal/domain/account"
	"github.com/vortex-trading/vortex-trading-engine/internal/domain/common"
	"github.com/vortex-trading/vortex-trading-engine/internal/domain/order"
)

type PlaceOrderCommand struct {
	ClientOrderID string
	AccountID     string
	Symbol        order.Symbol
	Side          order.OrderSide
	Type          order.OrderType
	Price         order.Price
	Quantity      order.Quantity
}

type PlaceOrderResult struct {
	Order   *order.Order
	Matches []order.OrderMatchedEvent
}

type PlaceOrderHandler struct {
	matcher     *order.Matcher
	accountRepo ports.AccountRepository
	eventBus    ports.EventPublisher
	idempotency ports.IdempotencyStore
}

func NewPlaceOrderHandler(
	matcher *order.Matcher,
	accountRepo ports.AccountRepository,
	eventBus ports.EventPublisher,
	idempotency ports.IdempotencyStore,
) *PlaceOrderHandler {
	return &PlaceOrderHandler{
		matcher:     matcher,
		accountRepo: accountRepo,
		eventBus:    eventBus,
		idempotency: idempotency,
	}
}

func (h *PlaceOrderHandler) Handle(ctx context.Context, cmd PlaceOrderCommand) (*PlaceOrderResult, error) {
	// 1. Idempotency check via Redis
	if h.idempotency != nil && cmd.ClientOrderID != "" {
		isNew, err := h.idempotency.CheckAndSet(ctx, fmt.Sprintf("order:idem:%s", cmd.ClientOrderID), 86400)
		if err != nil {
			return nil, err
		}
		if !isNew {
			return nil, common.ErrDuplicateClientOrderID
		}
	}

	// 2. Fetch Trader Account for Pre-Trade Risk & Sức Mua Validation
	acc, err := h.accountRepo.GetByID(ctx, cmd.AccountID)
	if err != nil {
		return nil, err
	}

	var reservedFunds int64
	if cmd.Side == order.SideBuy {
		// Lock required funds: Price * Qty * (1 + FeeRate)
		res, err := acc.ReserveBuyFunds(cmd.Price, cmd.Quantity)
		if err != nil {
			return nil, err
		}
		reservedFunds = res
	} else {
		// Lock stock shares
		err := acc.ReserveSellHolding(cmd.Symbol, cmd.Quantity)
		if err != nil {
			return nil, err
		}
	}

	orderID := fmt.Sprintf("ORD-%s", uuid.New().String()[:8])
	ord, err := order.NewOrder(
		orderID,
		cmd.ClientOrderID,
		cmd.AccountID,
		cmd.Symbol,
		cmd.Side,
		cmd.Type,
		cmd.Price,
		cmd.Quantity,
	)
	if err != nil {
		// Rollback reservation if order creation failed
		if cmd.Side == order.SideBuy {
			acc.ReleaseBuyFunds(reservedFunds)
		} else {
			acc.ReleaseSellHolding(cmd.Symbol, cmd.Quantity)
		}
		return nil, err
	}

	// 3. Execute in-memory matching engine (Price-Time Priority)
	matches, err := h.matcher.ProcessOrder(ord)
	if err != nil {
		return nil, err
	}

	// 4. Update account balances for executed fills
	for _, match := range matches {
		if ord.Side == order.SideBuy {
			acc.SettleBuyTrade(ord.Symbol, match.Price, match.Quantity, reservedFunds)
		} else {
			acc.SettleSellTrade(ord.Symbol, match.Price, match.Quantity)
		}
	}

	// 5. Emit events via EventBus (Watermill Kafka)
	if h.eventBus != nil {
		_ = h.eventBus.PublishOrderPlaced(ctx, order.OrderPlacedEvent{
			OrderID:       ord.ID,
			ClientOrderID: ord.ClientOrderID,
			AccountID:     ord.AccountID,
			Symbol:        ord.Symbol,
			Side:          ord.Side,
			Type:          ord.Type,
			Price:         ord.Price,
			Quantity:      ord.Quantity,
			Timestamp:     time.Now(),
		})

		for _, match := range matches {
			_ = h.eventBus.PublishOrderMatched(ctx, match)
		}

		// Emit L2 depth update
		depth := h.matcher.Book().GetL2Depth(10)
		_ = h.eventBus.PublishMarketDepth(ctx, depth)
	}

	return &PlaceOrderResult{
		Order:   ord,
		Matches: matches,
	}, nil
}
