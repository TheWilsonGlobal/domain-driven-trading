package account

import (
	"sync"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

const (
	DefaultFeeRateBasisPoints = 15 // 0.15% brokerage fee = 15 bps
)

// Account represents a trader's financial state with zero overdraft protection
type Account struct {
	mu           sync.RWMutex
	ID           string                  `json:"account_id"`
	CashBalance  int64                   `json:"cash_balance"`  // Cash in VND
	LockedCash   int64                   `json:"locked_cash"`   // Reserved for open BUY limit orders
	Holdings     map[order.Symbol]uint32 `json:"holdings"`      // Available stock shares
	LockedStocks map[order.Symbol]uint32 `json:"locked_stocks"` // Reserved for open SELL limit orders
	UpdatedAt    time.Time               `json:"updated_at"`
}

func NewAccount(id string, initialCash int64) *Account {
	return &Account{
		ID:           id,
		CashBalance:  initialCash,
		LockedCash:   0,
		Holdings:     make(map[order.Symbol]uint32),
		LockedStocks: make(map[order.Symbol]uint32),
		UpdatedAt:    time.Now(),
	}
}

// AvailableCash returns unreserved cash ("Sá»©c mua" - purchasing power without margin)
func (a *Account) AvailableCash() int64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.CashBalance <= a.LockedCash {
		return 0
	}
	return a.CashBalance - a.LockedCash
}

// CalculateRequiredFunds calculates Price * Qty * (1 + FeeRate)
func CalculateRequiredFunds(price order.Price, qty order.Quantity, feeBps int64) int64 {
	notional := price.Int64() * int64(qty.Uint32())
	fee := (notional * feeBps) / 10000
	return notional + fee
}

// ReserveBuyFunds performs pre-trade risk lock for BUY orders
func (a *Account) ReserveBuyFunds(price order.Price, qty order.Quantity) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	required := CalculateRequiredFunds(price, qty, DefaultFeeRateBasisPoints)
	available := a.CashBalance - a.LockedCash

	if available < required {
		return 0, common.ErrInsufficientBalance
	}

	a.LockedCash += required
	a.UpdatedAt = time.Now()
	return required, nil
}

// ReleaseBuyFunds releases reserved funds if order is canceled or partially unfulfilled
func (a *Account) ReleaseBuyFunds(amount int64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.LockedCash >= amount {
		a.LockedCash -= amount
	} else {
		a.LockedCash = 0
	}
	a.UpdatedAt = time.Now()
}

// ReserveSellHolding performs pre-trade risk lock for SELL orders
func (a *Account) ReserveSellHolding(symbol order.Symbol, qty order.Quantity) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	available := a.Holdings[symbol] - a.LockedStocks[symbol]
	if available < qty.Uint32() {
		return common.ErrInsufficientHolding
	}

	a.LockedStocks[symbol] += qty.Uint32()
	a.UpdatedAt = time.Now()
	return nil
}

// ReleaseSellHolding releases locked shares if SELL order is canceled
func (a *Account) ReleaseSellHolding(symbol order.Symbol, qty order.Quantity) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.LockedStocks[symbol] >= qty.Uint32() {
		a.LockedStocks[symbol] -= qty.Uint32()
	} else {
		a.LockedStocks[symbol] = 0
	}
	a.UpdatedAt = time.Now()
}

// SettleBuyTrade finalizes execution: unlocks cash, deducts cash, credits stock
func (a *Account) SettleBuyTrade(symbol order.Symbol, matchPrice order.Price, matchQty order.Quantity, reservedAmount int64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	actualCost := CalculateRequiredFunds(matchPrice, matchQty, DefaultFeeRateBasisPoints)

	// Release locked cash
	if a.LockedCash >= reservedAmount {
		a.LockedCash -= reservedAmount
	} else {
		a.LockedCash = 0
	}

	// Deduct actual cost from balance
	a.CashBalance -= actualCost
	// Credit stock holdings
	a.Holdings[symbol] += matchQty.Uint32()
	a.UpdatedAt = time.Now()
}

// SettleSellTrade finalizes execution: unlocks stock, removes stock, credits cash
func (a *Account) SettleSellTrade(symbol order.Symbol, matchPrice order.Price, matchQty order.Quantity) int64 {
	a.mu.Lock()
	defer a.mu.Unlock()

	notional := matchPrice.Int64() * int64(matchQty.Uint32())
	fee := (notional * DefaultFeeRateBasisPoints) / 10000
	netProceeds := notional - fee

	// Release locked stock and deduct from holding
	if a.LockedStocks[symbol] >= matchQty.Uint32() {
		a.LockedStocks[symbol] -= matchQty.Uint32()
	}
	if a.Holdings[symbol] >= matchQty.Uint32() {
		a.Holdings[symbol] -= matchQty.Uint32()
	}

	// Credit net cash proceeds
	a.CashBalance += netProceeds
	a.UpdatedAt = time.Now()
	return netProceeds
}

// DepositCash adds funds (e.g. initial funding or bank deposit)
func (a *Account) DepositCash(amount int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.CashBalance += amount
	a.UpdatedAt = time.Now()
}

// CreditStock grants stock holding (e.g. for testing or transfer)
func (a *Account) CreditStock(symbol order.Symbol, qty uint32) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Holdings[symbol] += qty
	a.UpdatedAt = time.Now()
}
