package account

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
)

const (
	DefaultFeeRateBasisPoints = 15 // 0.15% brokerage fee = 15 bps

	// FeeCollectorID is the account credited with the fees of both sides of every trade.
	FeeCollectorID = "FEE_COLLECTOR"
)

// Account represents a trader's financial state with zero overdraft protection
type Account struct {
	mu           sync.RWMutex
	ID           string                   `json:"account_id"`
	CashBalance  int64                    `json:"cash_balance"`  // Cash in VND
	LockedCash   int64                    `json:"locked_cash"`   // Reserved for open BUY limit orders
	Holdings     map[common.Symbol]uint32 `json:"holdings"`      // Available stock shares
	LockedStocks map[common.Symbol]uint32 `json:"locked_stocks"` // Reserved for open SELL limit orders
	UpdatedAt    time.Time                `json:"updated_at"`
}

func NewAccount(id string, initialCash int64) *Account {
	return &Account{
		ID:           id,
		CashBalance:  initialCash,
		LockedCash:   0,
		Holdings:     make(map[common.Symbol]uint32),
		LockedStocks: make(map[common.Symbol]uint32),
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

// mulCheck multiplies two non-negative int64 values, reporting overflow.
func mulCheck(a, b int64) (int64, bool) {
	if a < 0 || b < 0 {
		return 0, false
	}
	if a == 0 || b == 0 {
		return 0, true
	}
	if a > math.MaxInt64/b {
		return 0, false
	}
	return a * b, true
}

// notional returns price*qty with overflow detection.
func notional(price common.Price, qty common.Quantity) (int64, error) {
	n, ok := mulCheck(price.Int64(), int64(qty.Uint32()))
	if !ok {
		return 0, common.ErrArithmeticOverflow
	}
	return n, nil
}

// ceilFee returns ceil(n * feeBps / 10000) for a non-negative notional n.
// The caller has already proven n*(10000+feeBps) fits in int64.
func ceilFee(n, feeBps int64) int64 {
	scaled := n * feeBps
	fee := scaled / 10000
	if scaled%10000 != 0 {
		fee++
	}
	return fee
}

// Fee returns the brokerage fee of one execution: ceil(price*qty*bps/10000),
// computed with checked arithmetic. Reserve and settle both use it.
func Fee(price common.Price, qty common.Quantity) (int64, error) {
	n, err := notional(price, qty)
	if err != nil {
		return 0, err
	}
	if _, ok := mulCheck(n, 10000+DefaultFeeRateBasisPoints); !ok {
		return 0, common.ErrArithmeticOverflow
	}
	return ceilFee(n, DefaultFeeRateBasisPoints), nil
}

// CalculateRequiredFunds calculates Price * Qty + ceil(Price * Qty * FeeRate)
// using checked arithmetic: price*qty*(10000+feeBps) is computed without
// wrapping and an overflow yields ErrArithmeticOverflow. common.MaxPrice/MaxQty
// keep every legal order far below the int64 limit.
func CalculateRequiredFunds(price common.Price, qty common.Quantity, feeBps int64) (int64, error) {
	n, err := notional(price, qty)
	if err != nil {
		return 0, err
	}
	if _, ok := mulCheck(n, 10000+feeBps); !ok {
		return 0, common.ErrArithmeticOverflow
	}
	return n + ceilFee(n, feeBps), nil
}

// ReserveBuyFunds performs pre-trade risk lock for BUY orders
func (a *Account) ReserveBuyFunds(price common.Price, qty common.Quantity) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	required, err := CalculateRequiredFunds(price, qty, DefaultFeeRateBasisPoints)
	if err != nil {
		return 0, err
	}
	if required <= 0 {
		return 0, common.ErrInvalidPrice // never reserve 0
	}
	available := a.CashBalance - a.LockedCash

	if available < required {
		return 0, common.ErrInsufficientBalance
	}

	a.LockedCash += required
	a.UpdatedAt = time.Now()
	return required, nil
}

// ReleaseBuyFunds hands back reserved cash. It never clamps: releasing more
// than is locked means the books are inconsistent, so it returns
// ErrSettlementMismatch and leaves the account untouched.
func (a *Account) ReleaseBuyFunds(amount int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if amount < 0 || a.LockedCash < amount {
		return common.ErrSettlementMismatch
	}
	a.LockedCash -= amount
	a.UpdatedAt = time.Now()
	return nil
}

// CanReleaseBuyFunds reports whether ReleaseBuyFunds(amount) would succeed.
func (a *Account) CanReleaseBuyFunds(amount int64) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return amount >= 0 && a.LockedCash >= amount
}

// ReserveSellHolding performs pre-trade risk lock for SELL orders
func (a *Account) ReserveSellHolding(symbol common.Symbol, qty common.Quantity) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	held, locked := a.Holdings[symbol], a.LockedStocks[symbol]
	if locked > held || held-locked < qty.Uint32() {
		return common.ErrInsufficientHolding
	}

	a.LockedStocks[symbol] += qty.Uint32()
	a.UpdatedAt = time.Now()
	return nil
}

// ReleaseSellHolding releases locked shares (cancel / unfilled remainder).
// Like ReleaseBuyFunds it refuses to release more than is locked.
func (a *Account) ReleaseSellHolding(symbol common.Symbol, qty common.Quantity) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.LockedStocks[symbol] < qty.Uint32() {
		return common.ErrSettlementMismatch
	}
	a.LockedStocks[symbol] -= qty.Uint32()
	a.UpdatedAt = time.Now()
	return nil
}

// CanReleaseSellHolding reports whether ReleaseSellHolding(symbol, qty) would succeed.
func (a *Account) CanReleaseSellHolding(symbol common.Symbol, qty common.Quantity) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.LockedStocks[symbol] >= qty.Uint32()
}

// SettleBuyTrade finalizes one BUY execution: it unlocks unlock (this fill's
// share of the order's reservation, see Order.ApplyFill), debits the
// actual cost price*qty+fee and credits the shares. Price improvement and fee
// rounding are not debited, so they become available once the share is
// unlocked. It returns the fee charged. On error the account is untouched.
func (a *Account) SettleBuyTrade(symbol common.Symbol, matchPrice common.Price, matchQty common.Quantity, unlock int64) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	cost, err := CalculateRequiredFunds(matchPrice, matchQty, DefaultFeeRateBasisPoints)
	if err != nil {
		return 0, err
	}
	fee := cost - matchPrice.Int64()*int64(matchQty.Uint32())
	if unlock < 0 || a.LockedCash < unlock || a.CashBalance < cost {
		return 0, common.ErrSettlementMismatch
	}

	a.LockedCash -= unlock
	a.CashBalance -= cost
	a.Holdings[symbol] += matchQty.Uint32()
	a.UpdatedAt = time.Now()
	return fee, nil
}

// SettleSellTrade finalizes one SELL execution: unlocks stock, removes stock
// and credits price*qty minus the fee. It returns the net proceeds and the fee,
// or ErrSettlementMismatch, leaving the account untouched, when the account
// does not hold and lock at least matchQty shares.
func (a *Account) SettleSellTrade(symbol common.Symbol, matchPrice common.Price, matchQty common.Quantity) (int64, int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	n, err := notional(matchPrice, matchQty)
	if err != nil {
		return 0, 0, err
	}
	fee, err := Fee(matchPrice, matchQty)
	if err != nil {
		return 0, 0, err
	}
	netProceeds := n - fee

	q := matchQty.Uint32()
	if a.LockedStocks[symbol] < q || a.Holdings[symbol] < q {
		return 0, 0, common.ErrSettlementMismatch
	}
	a.LockedStocks[symbol] -= q
	a.Holdings[symbol] -= q

	a.CashBalance += netProceeds
	a.UpdatedAt = time.Now()
	return netProceeds, fee, nil
}

// DepositCash adds funds (e.g. initial funding or bank deposit)
func (a *Account) DepositCash(amount int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.CashBalance += amount
	a.UpdatedAt = time.Now()
}

// CreditStock grants stock holding (e.g. for testing or transfer)
func (a *Account) CreditStock(symbol common.Symbol, qty uint32) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Holdings[symbol] += qty
	a.UpdatedAt = time.Now()
}

// Snapshot returns a detached copy of the account taken under the read lock,
// safe to serialise while other requests mutate the live account.
func (a *Account) Snapshot() *Account {
	a.mu.RLock()
	defer a.mu.RUnlock()
	c := &Account{
		ID:           a.ID,
		CashBalance:  a.CashBalance,
		LockedCash:   a.LockedCash,
		Holdings:     make(map[common.Symbol]uint32, len(a.Holdings)),
		LockedStocks: make(map[common.Symbol]uint32, len(a.LockedStocks)),
		UpdatedAt:    a.UpdatedAt,
	}
	for k, v := range a.Holdings {
		c.Holdings[k] = v
	}
	for k, v := range a.LockedStocks {
		c.LockedStocks[k] = v
	}
	return c
}

// Restore rebuilds an account from persisted state. It is the only way for an
// adapter to materialise an account with non-zero locks: the fields stay
// exported for serialisation, but adapters must not write them directly.
// It rejects state no sequence of domain operations could have produced
// (negative amounts, a share lock above the holding).
func Restore(
	id string,
	cash, lockedCash int64,
	holdings, lockedStocks map[common.Symbol]uint32,
	updatedAt time.Time,
) (*Account, error) {
	if cash < 0 || lockedCash < 0 {
		return nil, fmt.Errorf("%w: account %s cash=%d locked=%d", common.ErrInvalidState, id, cash, lockedCash)
	}
	a := &Account{
		ID:           id,
		CashBalance:  cash,
		LockedCash:   lockedCash,
		Holdings:     make(map[common.Symbol]uint32, len(holdings)),
		LockedStocks: make(map[common.Symbol]uint32, len(lockedStocks)),
		UpdatedAt:    updatedAt,
	}
	for sym, q := range holdings {
		a.Holdings[sym] = q
	}
	for sym, q := range lockedStocks {
		if q > holdings[sym] {
			return nil, fmt.Errorf("%w: account %s locked %d > held %d of %s", common.ErrInvalidState, id, q, holdings[sym], sym)
		}
		a.LockedStocks[sym] = q
	}
	return a, nil
}

// ReplaceWith overwrites this account's state with src's, in place and under
// the account lock, so readers holding the pointer never see a stale object
// swapped out from under them. ids must match.
func (a *Account) ReplaceWith(src *Account) error {
	s := src.Snapshot()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ID != s.ID {
		return fmt.Errorf("%w: replace %s with %s", common.ErrInvalidState, a.ID, s.ID)
	}
	a.CashBalance = s.CashBalance
	a.LockedCash = s.LockedCash
	a.Holdings = s.Holdings
	a.LockedStocks = s.LockedStocks
	a.UpdatedAt = s.UpdatedAt
	return nil
}

// RestoreLocks sets the cash lock and the share lock of one symbol to values
// recomputed from the open orders (crash recovery). It refuses a lock the
// account could not back.
func (a *Account) RestoreLocks(lockedCash int64, symbol common.Symbol, lockedQty uint32) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if lockedCash < 0 || lockedQty > a.Holdings[symbol] {
		return fmt.Errorf("%w: account %s locks cash=%d %s=%d (held %d)",
			common.ErrInvalidState, a.ID, lockedCash, symbol, lockedQty, a.Holdings[symbol])
	}
	a.LockedCash = lockedCash
	if lockedQty == 0 {
		delete(a.LockedStocks, symbol)
	} else {
		a.LockedStocks[symbol] = lockedQty
	}
	a.UpdatedAt = time.Now()
	return nil
}
