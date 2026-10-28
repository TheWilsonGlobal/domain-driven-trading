package command

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
	"github.com/google/uuid"
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
	orders      ports.OrderRepository        // optional, see WithOrderStore
	journal     ports.TradeJournalRepository // optional, see WithOrderStore
}

func NewPlaceOrderHandler(
	matcher *order.Matcher,
	accountRepo ports.AccountRepository,
	eventBus ports.EventPublisher,
) *PlaceOrderHandler {
	return &PlaceOrderHandler{
		matcher:     matcher,
		accountRepo: accountRepo,
		eventBus:    eventBus,
	}
}

// idRe bounds account / client order / order ids: 1-64 chars of [A-Za-z0-9_-].
var idRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidID reports whether s is an acceptable account / order / client order id.
func ValidID(s string) bool { return idRe.MatchString(s) }

// marketBuyProtectionNum/Den express the MARKET BUY protection price as
// best ask x 11/10 (integer math, decision D3).
const (
	marketBuyProtectionNum = 11
	marketBuyProtectionDen = 10
)

// Validate checks every field of the command without touching any state.
// The symbol is only checked for format here; Handle compares it to the book.
func (c PlaceOrderCommand) Validate() error {
	if !c.Side.IsValid() {
		return common.ErrInvalidSide
	}
	if c.Type != order.TypeLimit && c.Type != order.TypeMarket {
		return common.ErrInvalidOrderType
	}
	if c.Quantity == 0 || c.Quantity > order.MaxQty {
		return common.ErrInvalidQuantity
	}
	if c.Type == order.TypeLimit {
		if c.Price <= 0 || c.Price > order.MaxPrice {
			return common.ErrInvalidPrice
		}
		if !c.Price.IsValidTick() {
			return common.ErrInvalidTickSize
		}
	}
	if !c.Symbol.IsValid() {
		return common.ErrInvalidSymbol
	}
	if !idRe.MatchString(c.AccountID) {
		return common.ErrInvalidAccountID
	}
	if c.ClientOrderID != "" && !idRe.MatchString(c.ClientOrderID) {
		return common.ErrInvalidClientOrderID
	}
	return nil
}

func (h *PlaceOrderHandler) Handle(ctx context.Context, cmd PlaceOrderCommand) (*PlaceOrderResult, error) {
	// 0. Validate before any state change (reservation, book).
	if err := cmd.Validate(); err != nil {
		return nil, err
	}
	if cmd.Symbol != h.matcher.Symbol() {
		return nil, common.ErrSymbolMismatch
	}

	// MARKET BUY is priced at the protection price (best ask x 1.1) so a
	// non-zero amount is reserved and the sweep cannot exceed the reservation.
	price := cmd.Price
	if cmd.Type == order.TypeMarket {
		price = 0
		if cmd.Side == order.SideBuy {
			ask, _, ok := h.matcher.BestAsk()
			if !ok {
				return nil, common.ErrNoLiquidity
			}
			if ask > order.MaxPrice {
				return nil, common.ErrArithmeticOverflow
			}
			price = ask * marketBuyProtectionNum / marketBuyProtectionDen
		}
	}

	// 2-4. Reserve -> match -> settle is one unit under the per-symbol engine lock.
	var (
		placed  *order.Order
		matches []order.OrderMatchedEvent
	)
	err := h.matcher.Atomically(func() error {
		var err error
		placed, matches, err = h.execute(ctx, cmd, price)
		return err
	})
	if err != nil {
		return nil, err
	}

	// 5. Emit events after the engine lock is released. The publisher never
	// blocks (bounded async queue with retry and a dead-letter log); an error
	// here means an event could not even be queued, which is logged, never
	// swallowed, and does not undo the executed order.
	if h.eventBus != nil {
		publishLog := func(kind string, err error) {
			if err != nil {
				slog.Error("event publish failed", "event", kind, "order_id", placed.ID, "error", err)
			}
		}
		publishLog("OrderPlaced", h.eventBus.PublishOrderPlaced(ctx, order.OrderPlacedEvent{
			OrderID:       placed.ID,
			ClientOrderID: placed.ClientOrderID,
			AccountID:     placed.AccountID,
			Symbol:        placed.Symbol,
			Side:          placed.Side,
			Type:          placed.Type,
			Price:         placed.Price,
			Quantity:      placed.Quantity,
			Timestamp:     time.Now(),
		}))

		for _, match := range matches {
			publishLog("OrderMatched", h.eventBus.PublishOrderMatched(ctx, match))
		}

		// Emit L2 depth update
		publishLog("MarketDepth", h.eventBus.PublishMarketDepth(ctx, h.matcher.Depth(10)))
	}

	return &PlaceOrderResult{
		Order:   placed, // snapshot taken under the book lock
		Matches: matches,
	}, nil
}

// WithOrderStore enables persistence of orders and of executions: placed and
// filled orders are saved through orders, and every execution is written with
// its double-entry journal in one transaction through journal. Either may be
// nil to leave that part off (tests, memory mode without recovery).
func (h *PlaceOrderHandler) WithOrderStore(orders ports.OrderRepository, journal ports.TradeJournalRepository) *PlaceOrderHandler {
	h.orders, h.journal = orders, journal
	return h
}

// execute reserves, matches, settles and persists one order. The caller holds
// the engine lock (Matcher.Atomically). It returns the taker snapshot and its
// executions.
//
// Persistence order: the reservation is saved BEFORE the order touches the book
// (a failure undoes it and aborts); once executions exist they cannot be undone,
// so settlement, orders and the trade+journal are all attempted and every error
// is returned together.
func (h *PlaceOrderHandler) execute(ctx context.Context, cmd PlaceOrderCommand, price order.Price) (*order.Order, []order.OrderMatchedEvent, error) {
	// Everything that can fail on lookup happens before any state changes.
	acc, err := resolveAccount(ctx, h.accountRepo, cmd.AccountID)
	if err != nil {
		return nil, nil, err
	}
	collector, err := resolveFeeCollector(ctx, h.accountRepo)
	if err != nil {
		return nil, nil, err
	}

	// Reserve: cash for a BUY (price * qty + fee), shares for a SELL.
	var reservedFunds int64
	if cmd.Side == order.SideBuy {
		reservedFunds, err = acc.ReserveBuyFunds(price, cmd.Quantity)
	} else {
		err = acc.ReserveSellHolding(cmd.Symbol, cmd.Quantity)
	}
	if err != nil {
		return nil, nil, err
	}
	undo := func() error {
		if cmd.Side == order.SideBuy {
			return acc.ReleaseBuyFunds(reservedFunds)
		}
		return acc.ReleaseSellHolding(cmd.Symbol, cmd.Quantity)
	}
	// rollback undoes the in-memory reservation and, when persist is true, saves
	// the undone state (the reservation had reached the store).
	rollback := func(cause error, persist bool) error {
		errs := []error{cause}
		if uerr := undo(); uerr != nil {
			errs = append(errs, uerr)
		} else if persist {
			if serr := h.accountRepo.Save(ctx, acc); serr != nil {
				errs = append(errs, serr)
			}
		}
		return joinErrs(errs)
	}

	if err := persistReserve(ctx, h.accountRepo, acc, cmd.Side, reservedFunds); err != nil {
		return nil, nil, rollback(err, false)
	}

	ord, err := order.NewOrder(uuid.New().String(), cmd.ClientOrderID, cmd.AccountID, cmd.Symbol, cmd.Side, cmd.Type, price, cmd.Quantity)
	if err != nil {
		return nil, nil, rollback(err, true)
	}
	if cmd.Side == order.SideBuy {
		ord.Reserve(reservedFunds)
	}

	// Execute in-memory matching engine (price-time priority).
	placed, matches, err := h.matcher.Place(ord)
	if err != nil {
		if len(matches) == 0 {
			return nil, nil, rollback(err, true) // nothing executed, undo the reservation
		}
		// Executions happened: settle them, then report the failure.
		makers, serr := settleMatches(ctx, h.accountRepo, collector, acc, matches)
		perr := h.persist(ctx, acc, collector, makers, ord.Snapshot(), matches)
		return nil, matches, errors.Join(err, serr, perr)
	}

	makers, settleErr := settleMatches(ctx, h.accountRepo, collector, acc, matches)

	// A terminated remainder (MARKET leftover, self-trade stop) never rests:
	// hand back whatever it still has reserved.
	var releaseErr error
	if settleErr == nil && isTerminal(placed.Status) {
		if err := releaseOrder(acc, placed); err != nil {
			releaseErr = fmt.Errorf("release unfilled remainder of %s: %w", placed.ID, err)
		}
	}

	// Save the accounts (reserve / settle / release state), the order and the
	// trades with their journal, even when settlement failed half-way.
	persistErr := h.persist(ctx, acc, collector, makers, placed, matches)
	if err := errors.Join(settleErr, releaseErr, persistErr); err != nil {
		return nil, matches, partiallyExecuted(err)
	}
	return placed, matches, nil
}

// partiallyExecuted tags a failure that happened once the order was in the book
// (or had matched): it can no longer be undone, so callers must treat it as
// indeterminate (no retry under the same idempotency key).
func partiallyExecuted(err error) error {
	return fmt.Errorf("%w: %w", common.ErrPartiallyExecuted, err)
}

func (h *PlaceOrderHandler) persist(ctx context.Context, taker, collector *account.Account, makers []*account.Account, placed *order.Order, matches []order.OrderMatchedEvent) error {
	touched := []*account.Account{taker}
	if len(matches) > 0 {
		touched = append(touched, collector)
		touched = append(touched, makers...)
	}
	return persistExecution(ctx, h.accountRepo, h.orders, h.journal, touched, placed, matches)
}
