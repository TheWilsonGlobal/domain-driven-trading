package seed

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

// MakerOrder is one resting limit order of the starter liquidity.
type MakerOrder struct {
	ClientOrderID string
	AccountID     string
	Side          order.OrderSide
	Price         order.Price
	Quantity      order.Quantity
}

// Placer is the part of PlaceOrderHandler the maker seeding needs.
type Placer interface {
	Handle(ctx context.Context, cmd command.PlaceOrderCommand) (*command.PlaceOrderResult, error)
}

// MakerOrders places the starter liquidity, once. openByAccount is the number of
// open orders per account after recovery: a maker that already has open orders
// is skipped entirely, so a restart never duplicates its quotes. A maker whose
// account does not exist (custom SEED_ACCOUNTS) is skipped with a warning; any
// other Handle error stops the run and is returned. It returns how many orders
// it placed.
func MakerOrders(
	ctx context.Context,
	place Placer,
	symbol order.Symbol,
	openByAccount map[string]int,
	specs []MakerOrder,
	logger *slog.Logger,
) (int, error) {
	if logger == nil {
		logger = slog.Default()
	}
	placed := 0
	for _, s := range specs {
		if openByAccount[s.AccountID] > 0 {
			logger.Info("seed: maker already has open orders, not seeding again",
				"account", s.AccountID, "client_order_id", s.ClientOrderID)
			continue
		}
		_, err := place.Handle(ctx, command.PlaceOrderCommand{
			ClientOrderID: s.ClientOrderID,
			AccountID:     s.AccountID,
			Symbol:        symbol,
			Side:          s.Side,
			Type:          order.TypeLimit,
			Price:         s.Price,
			Quantity:      s.Quantity,
		})
		switch {
		case err == nil:
			placed++
		case errors.Is(err, common.ErrAccountNotFound):
			logger.Warn("seed: maker account not configured, order skipped",
				"account", s.AccountID, "client_order_id", s.ClientOrderID)
		default:
			return placed, fmt.Errorf("seed maker order %s: %w", s.ClientOrderID, err)
		}
	}
	return placed, nil
}
