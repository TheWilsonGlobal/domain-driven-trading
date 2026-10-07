package order

import (
	"fmt"
	"sync/atomic"
	"time"
)

var tradeSeq uint64

func generateTradeID() string {
	seq := atomic.AddUint64(&tradeSeq, 1)
	return fmt.Sprintf("TRD-%d-%d", time.Now().UnixNano(), seq)
}

// Matcher performs continuous in-memory FIFO price-time matching
type Matcher struct {
	book *OrderBook
}

func NewMatcher(book *OrderBook) *Matcher {
	return &Matcher{
		book: book,
	}
}

func (m *Matcher) Book() *OrderBook {
	return m.book
}

// ProcessOrder matches an incoming taker order against resting maker orders in the book.
// Returns list of match events and whether the order was fully filled or placed in book.
func (m *Matcher) ProcessOrder(taker *Order) ([]OrderMatchedEvent, error) {
	m.book.mu.Lock()
	defer m.book.mu.Unlock()

	var events []OrderMatchedEvent

	if taker.Side == SideBuy {
		events = m.matchBuyOrder(taker)
	} else {
		events = m.matchSellOrder(taker)
	}

	// If taker has remaining quantity and is a LIMIT order, rest it in the book
	if taker.RemainingQuantity() > 0 && taker.Type == TypeLimit {
		_ = m.book.AddLimitOrder(taker)
	}

	return events, nil
}

func (m *Matcher) matchBuyOrder(taker *Order) []OrderMatchedEvent {
	var events []OrderMatchedEvent

	for taker.RemainingQuantity() > 0 {
		bestAskPrice, askQueue, exists := m.book.BestAsk()
		if !exists {
			break
		}

		// Price priority check for Limit Orders: Taker BUY price must be >= Maker Ask price
		if taker.Type == TypeLimit && taker.Price < bestAskPrice {
			break
		}

		// Match against FIFO queue at this price level
		currMaker := askQueue.Head
		for currMaker != nil && taker.RemainingQuantity() > 0 {
			nextMaker := currMaker.Next

			matchQty := minQty(taker.RemainingQuantity(), currMaker.RemainingQuantity())
			matchPrice := currMaker.Price // Maker sets the execution price

			// Apply fills
			taker.ApplyFill(matchQty)
			currMaker.ApplyFill(matchQty)

			event := OrderMatchedEvent{
				TradeID:        generateTradeID(),
				Symbol:         taker.Symbol,
				MakerOrderID:   currMaker.ID,
				TakerOrderID:   taker.ID,
				MakerAccountID: currMaker.AccountID,
				TakerAccountID: taker.AccountID,
				TakerSide:      taker.Side,
				Price:          matchPrice,
				Quantity:       matchQty,
				MakerRemaining: currMaker.RemainingQuantity(),
				TakerRemaining: taker.RemainingQuantity(),
				Timestamp:      time.Now(),
			}
			events = append(events, event)

			// If maker order is completely filled, remove from FIFO queue
			if currMaker.RemainingQuantity() == 0 {
				askQueue.Remove(currMaker)
				delete(m.book.orders, currMaker.ID)
			}

			currMaker = nextMaker
		}

		// If price level queue is empty, remove price level
		if askQueue.Count == 0 {
			delete(m.book.asksMap, bestAskPrice)
			m.book.removeSortedAsk(bestAskPrice)
		}
	}

	return events
}

func (m *Matcher) matchSellOrder(taker *Order) []OrderMatchedEvent {
	var events []OrderMatchedEvent

	for taker.RemainingQuantity() > 0 {
		bestBidPrice, bidQueue, exists := m.book.BestBid()
		if !exists {
			break
		}

		// Price priority check for Limit Orders: Taker SELL price must be <= Maker Bid price
		if taker.Type == TypeLimit && taker.Price > bestBidPrice {
			break
		}

		// Match against FIFO queue at this price level
		currMaker := bidQueue.Head
		for currMaker != nil && taker.RemainingQuantity() > 0 {
			nextMaker := currMaker.Next

			matchQty := minQty(taker.RemainingQuantity(), currMaker.RemainingQuantity())
			matchPrice := currMaker.Price // Maker sets the execution price

			// Apply fills
			taker.ApplyFill(matchQty)
			currMaker.ApplyFill(matchQty)

			event := OrderMatchedEvent{
				TradeID:        generateTradeID(),
				Symbol:         taker.Symbol,
				MakerOrderID:   currMaker.ID,
				TakerOrderID:   taker.ID,
				MakerAccountID: currMaker.AccountID,
				TakerAccountID: taker.AccountID,
				TakerSide:      taker.Side,
				Price:          matchPrice,
				Quantity:       matchQty,
				MakerRemaining: currMaker.RemainingQuantity(),
				TakerRemaining: taker.RemainingQuantity(),
				Timestamp:      time.Now(),
			}
			events = append(events, event)

			// If maker order is filled, remove from FIFO queue
			if currMaker.RemainingQuantity() == 0 {
				bidQueue.Remove(currMaker)
				delete(m.book.orders, currMaker.ID)
			}

			currMaker = nextMaker
		}

		// If price level is empty, remove price level
		if bidQueue.Count == 0 {
			delete(m.book.bidsMap, bestBidPrice)
			m.book.removeSortedBid(bestBidPrice)
		}
	}

	return events
}

func minQty(a, b Quantity) Quantity {
	if a < b {
		return a
	}
	return b
}
