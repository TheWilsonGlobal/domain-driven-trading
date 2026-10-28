package kafka

import (
	"errors"
	"fmt"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

// maxUnlock bounds a cash reservation share. The largest legitimate value is
// MaxPrice * MaxQty * 1.1 plus fees (about 1.1e18); anything near the int64
// limit is a corrupt or hostile event.
const maxUnlock int64 = 2_000_000_000_000_000_000

// ValidateMatchedEvent checks an OrderMatched event received from the bus
// before it is persisted: required fields, id/symbol shape, positive bounded
// price and quantity, non-negative unlock amounts. The consumer dead-letters
// events that fail.
func ValidateMatchedEvent(ev order.OrderMatchedEvent) error {
	ids := []struct{ name, v string }{
		{"trade_id", ev.TradeID},
		{"maker_order_id", ev.MakerOrderID},
		{"taker_order_id", ev.TakerOrderID},
		{"maker_account_id", ev.MakerAccountID},
		{"taker_account_id", ev.TakerAccountID},
	}
	for _, f := range ids {
		if f.v == "" {
			return fmt.Errorf("%s is required", f.name)
		}
		if !command.ValidID(f.v) {
			return fmt.Errorf("%s is malformed", f.name)
		}
	}
	if !ev.Symbol.IsValid() {
		return errors.New("symbol is malformed")
	}
	if !ev.TakerSide.IsValid() {
		return errors.New("taker_side must be BUY or SELL")
	}
	if ev.Price <= 0 || ev.Price > order.MaxPrice {
		return fmt.Errorf("price %d out of range", ev.Price)
	}
	if ev.Quantity == 0 || ev.Quantity > order.MaxQty {
		return fmt.Errorf("quantity %d out of range", ev.Quantity)
	}
	if ev.MakerRemaining > order.MaxQty || ev.TakerRemaining > order.MaxQty {
		return errors.New("remaining quantity out of range")
	}
	if ev.MakerUnlock < 0 || ev.MakerUnlock > maxUnlock || ev.TakerUnlock < 0 || ev.TakerUnlock > maxUnlock {
		return errors.New("unlock amount out of range")
	}
	if ev.Timestamp.IsZero() {
		return errors.New("timestamp is required")
	}
	return nil
}
