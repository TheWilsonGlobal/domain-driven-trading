package order

import (
	"sort"
	"sync"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
)

// OrderQueue represents a FIFO doubly-linked list of orders at a specific Price level
type OrderQueue struct {
	Price         Price
	Head          *Order
	Tail          *Order
	TotalQuantity Quantity
	Count         uint32
}

func NewOrderQueue(price Price) *OrderQueue {
	return &OrderQueue{
		Price: price,
	}
}

func (q *OrderQueue) Enqueue(o *Order) {
	o.Next = nil
	o.Prev = q.Tail

	if q.Tail != nil {
		q.Tail.Next = o
	} else {
		q.Head = o
	}
	q.Tail = o

	q.TotalQuantity += o.RemainingQuantity()
	q.Count++
}

// Fill records that qty of a resting order was executed, keeping the level's
// TotalQuantity equal to the sum of the remaining quantities.
func (q *OrderQueue) Fill(qty Quantity) {
	if q.TotalQuantity >= qty {
		q.TotalQuantity -= qty
	} else {
		q.TotalQuantity = 0
	}
}

func (q *OrderQueue) Remove(o *Order) {
	if o.Prev != nil {
		o.Prev.Next = o.Next
	} else {
		q.Head = o.Next
	}

	if o.Next != nil {
		o.Next.Prev = o.Prev
	} else {
		q.Tail = o.Prev
	}

	rem := o.RemainingQuantity()
	if q.TotalQuantity >= rem {
		q.TotalQuantity -= rem
	} else {
		q.TotalQuantity = 0
	}

	if q.Count > 0 {
		q.Count--
	}
	o.Prev = nil
	o.Next = nil
}

// OrderBook manages limit orders for a single symbol
type OrderBook struct {
	mu     sync.RWMutex
	symbol Symbol

	// Lookup maps
	orders map[string]*Order // order_id -> order pointer for O(1) cancel

	// Price levels
	bidsMap map[Price]*OrderQueue
	asksMap map[Price]*OrderQueue

	// Sorted price caches for fast matching
	sortedBids []Price // Descending (highest price first)
	sortedAsks []Price // Ascending (lowest price first)

	depthSeq int64
}

func NewOrderBook(symbol Symbol) *OrderBook {
	return &OrderBook{
		symbol:     symbol,
		orders:     make(map[string]*Order),
		bidsMap:    make(map[Price]*OrderQueue),
		asksMap:    make(map[Price]*OrderQueue),
		sortedBids: make([]Price, 0),
		sortedAsks: make([]Price, 0),
	}
}

func (ob *OrderBook) Symbol() Symbol {
	return ob.symbol
}

// admit rejects an order the book can never accept, before any state changes:
// wrong symbol or an id that is already resting. Callers must hold ob.mu.
func (ob *OrderBook) admit(o *Order) error {
	if o.Symbol != ob.symbol {
		return common.ErrSymbolMismatch
	}
	if _, exists := ob.orders[o.ID]; exists {
		return common.ErrOrderAlreadyExists
	}
	return nil
}

// AddLimitOrder adds an unmatched rest limit order to the book
func (ob *OrderBook) AddLimitOrder(o *Order) error {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	return ob.addLimitOrder(o)
}

// addLimitOrder is the unlocked implementation; callers must hold ob.mu.
func (ob *OrderBook) addLimitOrder(o *Order) error {
	if o.Symbol != ob.symbol {
		return common.ErrSymbolMismatch
	}
	if o.RemainingQuantity() == 0 {
		return nil
	}
	if _, exists := ob.orders[o.ID]; exists {
		return common.ErrOrderAlreadyExists
	}

	ob.orders[o.ID] = o

	if o.Side == SideBuy {
		q, exists := ob.bidsMap[o.Price]
		if !exists {
			q = NewOrderQueue(o.Price)
			ob.bidsMap[o.Price] = q
			ob.sortedBids = append(ob.sortedBids, o.Price)
			sort.Slice(ob.sortedBids, func(i, j int) bool {
				return ob.sortedBids[i] > ob.sortedBids[j] // Descending
			})
		}
		q.Enqueue(o)
	} else {
		q, exists := ob.asksMap[o.Price]
		if !exists {
			q = NewOrderQueue(o.Price)
			ob.asksMap[o.Price] = q
			ob.sortedAsks = append(ob.sortedAsks, o.Price)
			sort.Slice(ob.sortedAsks, func(i, j int) bool {
				return ob.sortedAsks[i] < ob.sortedAsks[j] // Ascending
			})
		}
		q.Enqueue(o)
	}

	return nil
}

// CancelOrder removes an active order by ID in O(1).
// The returned order is a snapshot, safe to read without the book lock.
func (ob *OrderBook) CancelOrder(orderID string) (*Order, error) {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	o, err := ob.cancelOrder(orderID)
	if err != nil {
		return nil, err
	}
	return o.Snapshot(), nil
}

// Get returns a snapshot of a resting order, or false if it is not in the book.
func (ob *OrderBook) Get(orderID string) (*Order, bool) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	o, ok := ob.orders[orderID]
	if !ok {
		return nil, false
	}
	return o.Snapshot(), true
}

// cancelOrder is the unlocked implementation; callers must hold ob.mu.
func (ob *OrderBook) cancelOrder(orderID string) (*Order, error) {
	o, exists := ob.orders[orderID]
	if !exists {
		return nil, common.ErrOrderNotFound
	}

	if err := o.Cancel(); err != nil {
		return nil, err
	}

	delete(ob.orders, orderID)

	if o.Side == SideBuy {
		if q, ok := ob.bidsMap[o.Price]; ok {
			q.Remove(o)
			if q.Count == 0 {
				delete(ob.bidsMap, o.Price)
				ob.removeSortedBid(o.Price)
			}
		}
	} else {
		if q, ok := ob.asksMap[o.Price]; ok {
			q.Remove(o)
			if q.Count == 0 {
				delete(ob.asksMap, o.Price)
				ob.removeSortedAsk(o.Price)
			}
		}
	}

	return o, nil
}

func (ob *OrderBook) removeSortedBid(p Price) {
	for i, price := range ob.sortedBids {
		if price == p {
			ob.sortedBids = append(ob.sortedBids[:i], ob.sortedBids[i+1:]...)
			return
		}
	}
}

func (ob *OrderBook) removeSortedAsk(p Price) {
	for i, price := range ob.sortedAsks {
		if price == p {
			ob.sortedAsks = append(ob.sortedAsks[:i], ob.sortedAsks[i+1:]...)
			return
		}
	}
}

// BestBid returns the highest buy price and the total resting quantity at it.
func (ob *OrderBook) BestBid() (Price, Quantity, bool) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	p, q, ok := ob.bestBid()
	if !ok {
		return 0, 0, false
	}
	return p, q.TotalQuantity, true
}

// BestAsk returns the lowest sell price and the total resting quantity at it.
func (ob *OrderBook) BestAsk() (Price, Quantity, bool) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	p, q, ok := ob.bestAsk()
	if !ok {
		return 0, 0, false
	}
	return p, q.TotalQuantity, true
}

// bestBid returns highest buy price and its queue; callers must hold ob.mu.
func (ob *OrderBook) bestBid() (Price, *OrderQueue, bool) {
	if len(ob.sortedBids) == 0 {
		return 0, nil, false
	}
	p := ob.sortedBids[0]
	return p, ob.bidsMap[p], true
}

// bestAsk returns lowest sell price and its queue; callers must hold ob.mu.
func (ob *OrderBook) bestAsk() (Price, *OrderQueue, bool) {
	if len(ob.sortedAsks) == 0 {
		return 0, nil, false
	}
	p := ob.sortedAsks[0]
	return p, ob.asksMap[p], true
}

// GetL2Depth returns Level 2 aggregated market depth up to maxLevels.
// It takes the write lock because it increments depthSeq.
func (ob *OrderBook) GetL2Depth(maxLevels int) MarketDepthEvent {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	if maxLevels < 0 {
		maxLevels = 0
	}
	ob.depthSeq++

	bids := make([]PriceLevelDepth, 0, maxLevels)
	for i := 0; i < len(ob.sortedBids) && i < maxLevels; i++ {
		p := ob.sortedBids[i]
		q := ob.bidsMap[p]
		if q != nil && q.TotalQuantity > 0 {
			bids = append(bids, PriceLevelDepth{
				Price:         p,
				TotalQuantity: q.TotalQuantity,
				OrderCount:    q.Count,
			})
		}
	}

	asks := make([]PriceLevelDepth, 0, maxLevels)
	for i := 0; i < len(ob.sortedAsks) && i < maxLevels; i++ {
		p := ob.sortedAsks[i]
		q := ob.asksMap[p]
		if q != nil && q.TotalQuantity > 0 {
			asks = append(asks, PriceLevelDepth{
				Price:         p,
				TotalQuantity: q.TotalQuantity,
				OrderCount:    q.Count,
			})
		}
	}

	return MarketDepthEvent{
		Symbol:    ob.symbol,
		Sequence:  ob.depthSeq,
		Bids:      bids,
		Asks:      asks,
		Timestamp: time.Now(),
	}
}
