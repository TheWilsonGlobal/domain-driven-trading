package common

import (
	"fmt"
	"regexp"
)

// Price represents currency ticks in VND (64-bit integer to prevent floating-point drift)
type Price int64

func (p Price) Int64() int64 {
	return int64(p)
}

func (p Price) String() string {
	return fmt.Sprintf("%d VND", int64(p))
}

// Quantity represents stock lot units (unsigned 32-bit integer)
type Quantity uint32

func (q Quantity) Uint32() uint32 {
	return uint32(q)
}

// Order limits. MaxPrice*MaxQty*(10000+feeBps) = 1e8*1e6*10015 ~ 1.0e18, and the
// MARKET BUY protection price (ask*1.1) gives ~1.1e18; both fit int64 (9.22e18).
const (
	TickSize Price    = 10          // VND, every limit price must be a multiple
	MaxPrice Price    = 100_000_000 // VND
	MaxQty   Quantity = 1_000_000
)

// IsValidTick reports whether the price is a positive multiple of TickSize.
func (p Price) IsValidTick() bool {
	return p > 0 && p%TickSize == 0
}

type Symbol string

var symbolRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

// IsValid checks the symbol format (2-10 chars, upper-case alphanumeric).
func (s Symbol) IsValid() bool { return symbolRe.MatchString(string(s)) }

func (s Symbol) String() string {
	return string(s)
}
