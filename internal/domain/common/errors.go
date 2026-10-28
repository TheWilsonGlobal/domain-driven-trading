package common

import "errors"

var (
	ErrInsufficientBalance    = errors.New("domain: insufficient purchasing power / cash balance")
	ErrInsufficientHolding    = errors.New("domain: insufficient stock asset holding")
	ErrInvalidPrice           = errors.New("domain: order price must be strictly positive (> 0)")
	ErrInvalidQuantity        = errors.New("domain: order quantity must be strictly positive (> 0)")
	ErrInvalidTickSize        = errors.New("domain: order price violates symbol tick size rules")
	ErrOrderNotFound          = errors.New("domain: order not found in active order book")
	ErrOrderAlreadyExists     = errors.New("domain: order with this ID already registered")
	ErrOrderCompleted         = errors.New("domain: cannot cancel an order that is already filled or canceled")
	ErrSymbolMismatch         = errors.New("domain: order symbol does not match target order book")
	ErrDuplicateClientOrderID = errors.New("domain: client order ID already processed (idempotency violation)")
	ErrInvalidSide            = errors.New("domain: order side must be BUY or SELL")
	ErrInvalidOrderType       = errors.New("domain: order type must be LIMIT or MARKET")
	ErrInvalidSymbol          = errors.New("domain: symbol is malformed")
	ErrInvalidAccountID       = errors.New("domain: account id is malformed")
	ErrInvalidClientOrderID   = errors.New("domain: client order id is malformed")
	ErrInvalidOrderID         = errors.New("domain: order id is malformed")
	ErrArithmeticOverflow     = errors.New("domain: monetary arithmetic overflow")
	ErrNoLiquidity            = errors.New("domain: no opposing liquidity for market order")
	ErrSettlementMismatch     = errors.New("domain: settlement does not match reserved position")
	ErrNegativeLedgerAmount   = errors.New("domain: double-entry amount must be positive")
	ErrAccountNotFound        = errors.New("domain: account not found")
	ErrFeeCollectorMissing    = errors.New("domain: fee collector account is not available")
	ErrInvalidState           = errors.New("domain: restored state violates an invariant")
	// ErrPartiallyExecuted marks a failure AFTER the order reached the book or
	// matched: its effects cannot be undone, so the outcome is indeterminate and
	// the request must not be retried under the same idempotency key.
	ErrPartiallyExecuted = errors.New("domain: order executed but follow-up processing failed")
	ErrUnbalancedLedger  = errors.New("domain: total debits must equal total credits in transaction")
)
