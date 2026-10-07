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
	ErrNegativeLedgerAmount   = errors.New("domain: double-entry amount must be positive")
	ErrUnbalancedLedger       = errors.New("domain: total debits must equal total credits in transaction")
)
