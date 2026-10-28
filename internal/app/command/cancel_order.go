package command

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
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
	orders      ports.OrderRepository // optional, see WithOrderStore
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

// WithOrderStore makes cancel persist the CANCELED order (nil leaves it off).
func (h *CancelOrderHandler) WithOrderStore(orders ports.OrderRepository) *CancelOrderHandler {
	h.orders = orders
	return h
}

// restoreAccount puts the in-memory account back to its pre-command state, in
// place, and re-saves it so the store matches again. The error is the re-save
// failure, if any.
func restoreAccount(ctx context.Context, repo ports.AccountRepository, acc, before *account.Account) error {
	if err := acc.ReplaceWith(before); err != nil {
		return err
	}
	if err := repo.Save(ctx, acc); err != nil {
		return fmt.Errorf("re-save restored account %s: %w", acc.ID, err)
	}
	return nil
}

// Validate checks id formats without touching any state.
func (c CancelOrderCommand) Validate() error {
	if !idRe.MatchString(c.OrderID) {
		return common.ErrInvalidOrderID
	}
	if !idRe.MatchString(c.AccountID) {
		return common.ErrInvalidAccountID
	}
	return nil
}

// Handle cancels a resting order on behalf of cmd.AccountID.
//
// The order is looked up first and must belong to the caller: an unknown order
// and somebody else's order both yield ErrOrderNotFound, so the response never
// reveals which ids exist. The owner's account is resolved and the release is
// proven possible before the order leaves the book, all under the engine lock,
// so a repository failure or an inconsistent lock leaves the order resting and
// the error is returned (never swallowed).
func (h *CancelOrderHandler) Handle(ctx context.Context, cmd CancelOrderCommand) (*order.Order, error) {
	if err := cmd.Validate(); err != nil {
		return nil, err
	}

	var canceledOrder *order.Order
	err := h.matcher.Atomically(func() error {
		resting, ok := h.matcher.Get(cmd.OrderID)
		if !ok || resting.AccountID != cmd.AccountID {
			return common.ErrOrderNotFound
		}
		acc, err := resolveAccount(ctx, h.accountRepo, resting.AccountID)
		if err != nil {
			return err
		}
		if !canReleaseOrder(acc, resting) {
			return fmt.Errorf("cancel %s: %w", resting.ID, common.ErrSettlementMismatch)
		}

		// Release and persist BEFORE the order leaves the book: if the store
		// refuses, the lock is restored in place and the order keeps resting.
		before := acc.Snapshot()
		if err := releaseOrder(acc, resting); err != nil {
			return fmt.Errorf("release canceled order %s: %w", resting.ID, err)
		}
		canceled := resting.Snapshot()
		if err := canceled.Cancel(); err != nil {
			return errors.Join(err, restoreAccount(ctx, h.accountRepo, acc, before))
		}
		if err := h.accountRepo.Save(ctx, acc); err != nil {
			return errors.Join(fmt.Errorf("save account %s: %w", acc.ID, err), restoreAccount(ctx, h.accountRepo, acc, before))
		}
		if h.orders != nil {
			if err := h.orders.Save(ctx, canceled); err != nil {
				return errors.Join(fmt.Errorf("save order %s: %w", canceled.ID, err), restoreAccount(ctx, h.accountRepo, acc, before))
			}
		}
		c, err := h.matcher.Cancel(cmd.OrderID)
		if err != nil {
			return errors.Join(err, restoreAccount(ctx, h.accountRepo, acc, before))
		}
		canceledOrder = c
		return nil
	})
	if err != nil {
		return nil, err
	}

	if h.eventBus != nil {
		logPublishErr("OrderCanceled", canceledOrder.ID, h.eventBus.PublishOrderCanceled(ctx, order.OrderCanceledEvent{
			OrderID:          canceledOrder.ID,
			AccountID:        canceledOrder.AccountID,
			Symbol:           canceledOrder.Symbol,
			CanceledQuantity: canceledOrder.RemainingQuantity(),
			Reason:           "USER_REQUESTED",
			Timestamp:        time.Now(),
		}))

		logPublishErr("MarketDepth", canceledOrder.ID, h.eventBus.PublishMarketDepth(ctx, h.matcher.Depth(10)))
	}

	return canceledOrder, nil
}

// logPublishErr reports an event that could not even be queued; the publisher
// retries and dead-letters on its own, so the order result is unaffected.
func logPublishErr(kind, orderID string, err error) {
	if err != nil {
		slog.Error("event publish failed", "event", kind, "order_id", orderID, "error", err)
	}
}
