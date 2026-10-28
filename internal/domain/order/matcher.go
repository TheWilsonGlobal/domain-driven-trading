package order

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
)

var tradeSeq uint64

func generateTradeID() string {
	seq := atomic.AddUint64(&tradeSeq, 1)
	return fmt.Sprintf("TRD-%d-%d", time.Now().UnixNano(), seq)
}

// Matcher performs continuous in-memory FIFO price-time matching
type Matcher struct {
	engine sync.Mutex // serialises reserve -> match -> settle and cancel (see Atomically)
	book   *OrderBook
}

func NewMatcher(book *OrderBook) *Matcher {
	return &Matcher{
		book: book,
	}
}

// ProcessOrder matches an incoming taker order against resting maker orders in the book.
// Returns list of match events and whether the order was fully filled or placed in book.
func (m *Matcher) ProcessOrder(taker *Order) ([]OrderMatchedEvent, error) {
	m.book.mu.Lock()
	defer m.book.mu.Unlock()
	if err := m.book.admit(taker); err != nil {
		return nil, err
	}
	return m.processOrder(taker)
}

// Place is ProcessOrder plus a snapshot of the taker taken under the book lock,
// so the caller can read it while other requests keep mutating the live order.
func (m *Matcher) Place(taker *Order) (*Order, []OrderMatchedEvent, error) {
	m.book.mu.Lock()
	defer m.book.mu.Unlock()
	if err := m.book.admit(taker); err != nil {
		return nil, nil, err
	}
	events, err := m.processOrder(taker)
	if err != nil {
		return nil, events, err
	}
	return taker.Snapshot(), events, nil
}

// processOrder is the unlocked matching routine; callers must hold book.mu.
// admit has already vetted symbol and id, so a resting failure here is unexpected
// and is returned rather than swallowed.
//
// Self-trade prevention is cancel-newest: when the next resting order belongs
// to the taker's own account, matching stops, no trade is produced against it,
// the resting order is left untouched and the taker's unfilled remainder is
// terminated (REJECTED if nothing filled, CANCELED otherwise) instead of resting.
// Fills against other accounts that precede the own order still execute.
//
// A MARKET remainder never rests either and is terminated the same way.
func (m *Matcher) processOrder(taker *Order) ([]OrderMatchedEvent, error) {
	var events []OrderMatchedEvent
	var selfTrade bool

	if taker.Side == SideBuy {
		events, selfTrade = m.matchBuyOrder(taker)
	} else {
		events, selfTrade = m.matchSellOrder(taker)
	}

	if taker.RemainingQuantity() > 0 {
		if selfTrade || taker.Type != TypeLimit {
			taker.CloseRemainder()
		} else if err := m.book.addLimitOrder(taker); err != nil {
			return events, err
		}
	}

	return events, nil
}

// Atomically runs fn while holding the engine lock of this symbol. Reserve,
// match and settle (and cancel with its release) run inside it so no other
// command observes a half-applied order. Lock order is always engine lock
// then book lock; fn may call any Matcher method but must not call Atomically.
func (m *Matcher) Atomically(fn func() error) error {
	m.engine.Lock()
	defer m.engine.Unlock()
	return fn()
}

// RestoreResting puts previously persisted open orders back into the book, in
// the given order (the caller passes them in time priority, so FIFO within a
// price level is preserved). Restored orders are never matched against each
// other: they rested together before the restart. It fails, leaving the orders
// restored so far in place, on an order that cannot rest.
func (m *Matcher) RestoreResting(orders []*Order) error {
	m.book.mu.Lock()
	defer m.book.mu.Unlock()
	for _, o := range orders {
		if !o.IsOpen() {
			return fmt.Errorf("order %s (%s) cannot rest: %w", o.ID, o.Status, common.ErrInvalidState)
		}
		if err := m.book.addLimitOrder(o); err != nil {
			return fmt.Errorf("restore order %s: %w", o.ID, err)
		}
	}
	return nil
}

// Symbol returns the symbol of the underlying book.
func (m *Matcher) Symbol() Symbol { return m.book.symbol }

// Cancel removes a resting order and returns a snapshot of it.
func (m *Matcher) Cancel(orderID string) (*Order, error) { return m.book.CancelOrder(orderID) }

// Depth returns aggregated L2 market depth.
func (m *Matcher) Depth(maxLevels int) MarketDepthEvent { return m.book.GetL2Depth(maxLevels) }

// Get returns a snapshot of a resting order.
func (m *Matcher) Get(orderID string) (*Order, bool) { return m.book.Get(orderID) }

// BestBid returns the best bid price and the quantity resting at it.
func (m *Matcher) BestBid() (Price, Quantity, bool) { return m.book.BestBid() }

// BestAsk returns the best ask price and the quantity resting at it.
func (m *Matcher) BestAsk() (Price, Quantity, bool) { return m.book.BestAsk() }

func (m *Matcher) matchBuyOrder(taker *Order) ([]OrderMatchedEvent, bool) {
	return m.match(taker, func() (Price, *OrderQueue, bool) { return m.book.bestAsk() },
		func(p Price) bool { return taker.Price < p },
		func(p Price) {
			delete(m.book.asksMap, p)
			m.book.removeSortedAsk(p)
		})
}

func (m *Matcher) matchSellOrder(taker *Order) ([]OrderMatchedEvent, bool) {
	return m.match(taker, func() (Price, *OrderQueue, bool) { return m.book.bestBid() },
		func(p Price) bool { return taker.Price > p },
		func(p Price) {
			delete(m.book.bidsMap, p)
			m.book.removeSortedBid(p)
		})
}

// match sweeps the opposite side. best returns the best opposite level,
// outOfLimit reports whether that level's price is beyond the taker's limit
// (or protection) price, dropLevel removes an emptied level. The second result
// is true when matching stopped because of a self-trade.
func (m *Matcher) match(
	taker *Order,
	best func() (Price, *OrderQueue, bool),
	outOfLimit func(Price) bool,
	dropLevel func(Price),
) ([]OrderMatchedEvent, bool) {
	var events []OrderMatchedEvent

	for taker.RemainingQuantity() > 0 {
		levelPrice, queue, exists := best()
		if !exists {
			break
		}

		// Price priority for limit orders and priced MARKET orders (protection price).
		if (taker.Type == TypeLimit || taker.Price > 0) && outOfLimit(levelPrice) {
			break
		}

		// Match against the FIFO queue at this price level.
		selfTrade := false
		for curr := queue.Head; curr != nil && taker.RemainingQuantity() > 0; {
			if curr.AccountID == taker.AccountID {
				selfTrade = true
				break
			}
			next := curr.Next

			matchQty := minQty(taker.RemainingQuantity(), curr.RemainingQuantity())
			matchPrice := curr.Price // Maker sets the execution price

			takerUnlock := taker.ApplyFill(matchQty)
			makerUnlock := curr.ApplyFill(matchQty)
			queue.Fill(matchQty)

			events = append(events, OrderMatchedEvent{
				TradeID:        generateTradeID(),
				Symbol:         taker.Symbol,
				MakerOrderID:   curr.ID,
				TakerOrderID:   taker.ID,
				MakerAccountID: curr.AccountID,
				TakerAccountID: taker.AccountID,
				TakerSide:      taker.Side,
				Price:          matchPrice,
				Quantity:       matchQty,
				MakerRemaining: curr.RemainingQuantity(),
				TakerRemaining: taker.RemainingQuantity(),
				MakerUnlock:    makerUnlock,
				TakerUnlock:    takerUnlock,
				Timestamp:      time.Now(),
			})

			// A completely filled maker leaves the queue and the book.
			if curr.RemainingQuantity() == 0 {
				queue.Remove(curr)
				delete(m.book.orders, curr.ID)
			}
			curr = next
		}

		if queue.Count == 0 {
			dropLevel(levelPrice)
		}
		if selfTrade {
			return events, true
		}
	}

	return events, false
}

func minQty(a, b Quantity) Quantity {
	if a < b {
		return a
	}
	return b
}
