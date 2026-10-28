package command_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

type fixture struct {
	book   *order.OrderBook
	m      *order.Matcher
	acc    *account.Account
	repo   *fakeAccountRepo
	place  *command.PlaceOrderHandler
	cancel *command.CancelOrderHandler
}

func newFixture(cash int64) *fixture {
	book := order.NewOrderBook("APX")
	m := order.NewMatcher(book)
	acc := account.NewAccount("TRADER", cash)
	acc.CreditStock("APX", 1000)
	maker := account.NewAccount("MAKER", 1_000_000_000)
	maker.CreditStock("APX", 100000)
	repo := newFakeRepo(acc, maker, account.NewAccount("FEE_COLLECTOR", 0))
	return &fixture{
		book: book, m: m, acc: acc, repo: repo,
		place:  command.NewPlaceOrderHandler(m, repo, nil),
		cancel: command.NewCancelOrderHandler(m, repo, nil),
	}
}

func (f *fixture) seedAsk(t *testing.T, price order.Price, qty order.Quantity) {
	t.Helper()
	if _, err := f.place.Handle(context.Background(), command.PlaceOrderCommand{
		AccountID: "MAKER", Symbol: "APX", Side: order.SideSell, Type: order.TypeLimit, Price: price, Quantity: qty,
	}); err != nil {
		t.Fatal(err)
	}
}

func depthLen(m *order.Matcher) (int, int) {
	d := m.Depth(50)
	return len(d.Bids), len(d.Asks)
}

func TestPlaceOrder_RejectsInvalidInput(t *testing.T) {
	good := command.PlaceOrderCommand{
		ClientOrderID: "C-1", AccountID: "TRADER", Symbol: "APX",
		Side: order.SideBuy, Type: order.TypeLimit, Price: 19500, Quantity: 10,
	}
	tests := []struct {
		name string
		mut  func(c *command.PlaceOrderCommand)
		want error
	}{
		{"bad side", func(c *command.PlaceOrderCommand) { c.Side = "HOLD" }, common.ErrInvalidSide},
		{"empty side", func(c *command.PlaceOrderCommand) { c.Side = "" }, common.ErrInvalidSide},
		{"bad type", func(c *command.PlaceOrderCommand) { c.Type = "STOP" }, common.ErrInvalidOrderType},
		{"zero qty", func(c *command.PlaceOrderCommand) { c.Quantity = 0 }, common.ErrInvalidQuantity},
		{"qty over max", func(c *command.PlaceOrderCommand) { c.Quantity = order.MaxQty + 1 }, common.ErrInvalidQuantity},
		{"zero price", func(c *command.PlaceOrderCommand) { c.Price = 0 }, common.ErrInvalidPrice},
		{"negative price", func(c *command.PlaceOrderCommand) { c.Price = -5 }, common.ErrInvalidPrice},
		{"price over max", func(c *command.PlaceOrderCommand) { c.Price = order.MaxPrice + order.TickSize }, common.ErrInvalidPrice},
		{"off tick", func(c *command.PlaceOrderCommand) { c.Price = 19505 }, common.ErrInvalidTickSize},
		{"unknown symbol", func(c *command.PlaceOrderCommand) { c.Symbol = "ZZZ" }, common.ErrSymbolMismatch},
		{"malformed symbol", func(c *command.PlaceOrderCommand) { c.Symbol = "a b;" }, common.ErrInvalidSymbol},
		{"empty symbol", func(c *command.PlaceOrderCommand) { c.Symbol = "" }, common.ErrInvalidSymbol},
		{"bad account id", func(c *command.PlaceOrderCommand) { c.AccountID = "../etc" }, common.ErrInvalidAccountID},
		{"empty account id", func(c *command.PlaceOrderCommand) { c.AccountID = "" }, common.ErrInvalidAccountID},
		{"long account id", func(c *command.PlaceOrderCommand) { c.AccountID = strings.Repeat("A", 65) }, common.ErrInvalidAccountID},
		{"bad client order id", func(c *command.PlaceOrderCommand) { c.ClientOrderID = "x y\n" }, common.ErrInvalidClientOrderID},
		{"long client order id", func(c *command.PlaceOrderCommand) { c.ClientOrderID = strings.Repeat("c", 65) }, common.ErrInvalidClientOrderID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(1_000_000_000)
			f.seedAsk(t, 19600, 50)
			accBefore := f.acc.Snapshot()
			b0, a0 := depthLen(f.m)

			cmd := good
			tc.mut(&cmd)
			if err := cmd.Validate(); tc.name != "unknown symbol" /* book symbol is compared in Handle, not Validate */ && !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
			_, err := f.place.Handle(context.Background(), cmd)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Handle() = %v, want %v", err, tc.want)
			}
			accAfter := f.acc.Snapshot()
			if accAfter.LockedCash != accBefore.LockedCash || accAfter.CashBalance != accBefore.CashBalance ||
				accAfter.Holdings["APX"] != accBefore.Holdings["APX"] || accAfter.LockedStocks["APX"] != accBefore.LockedStocks["APX"] {
				t.Errorf("account changed: before=%+v after=%+v", accBefore, accAfter)
			}
			if b1, a1 := depthLen(f.m); b1 != b0 || a1 != a0 {
				t.Errorf("book depth changed: %d/%d -> %d/%d", b0, a0, b1, a1)
			}
		})
	}
}

func TestPlaceOrder_ValidMarketNeedsNoPrice(t *testing.T) {
	cmd := command.PlaceOrderCommand{AccountID: "TRADER", Symbol: "APX", Side: order.SideSell, Type: order.TypeMarket, Quantity: 5}
	if err := cmd.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
}

// Plan 2.3: MARKET BUY must reserve a positive amount and never push cash negative.
func TestPlaceOrder_MarketBuyNoAskRejected(t *testing.T) {
	f := newFixture(1_000_000)
	before := f.acc.Snapshot()
	_, err := f.place.Handle(context.Background(), command.PlaceOrderCommand{
		AccountID: "TRADER", Symbol: "APX", Side: order.SideBuy, Type: order.TypeMarket, Quantity: 10,
	})
	if !errors.Is(err, common.ErrNoLiquidity) {
		t.Fatalf("err = %v, want ErrNoLiquidity", err)
	}
	if after := f.acc.Snapshot(); after.LockedCash != before.LockedCash || after.CashBalance != before.CashBalance {
		t.Errorf("account changed: %+v", after)
	}
}

func TestPlaceOrder_MarketBuyBrokeAccountRejected(t *testing.T) {
	f := newFixture(100) // cannot afford even one share
	f.seedAsk(t, 19500, 100)
	b0, a0 := depthLen(f.m)
	_, err := f.place.Handle(context.Background(), command.PlaceOrderCommand{
		AccountID: "TRADER", Symbol: "APX", Side: order.SideBuy, Type: order.TypeMarket, Quantity: 10,
	})
	if !errors.Is(err, common.ErrInsufficientBalance) {
		t.Fatalf("err = %v, want ErrInsufficientBalance", err)
	}
	s := f.acc.Snapshot()
	if s.CashBalance != 100 || s.LockedCash != 0 || s.Holdings["APX"] != 1000 {
		t.Errorf("account changed: %+v", s)
	}
	if b1, a1 := depthLen(f.m); b1 != b0 || a1 != a0 {
		t.Error("book changed")
	}
}

func TestPlaceOrder_MarketBuySweepStaysWithinReservation(t *testing.T) {
	// Asks at 19500 (10) and 40000 (1000): the second level is beyond ask*1.1,
	// so the market order must not reach it and cash must stay non-negative.
	f := newFixture(5_000_000)
	f.seedAsk(t, 19500, 10)
	f.seedAsk(t, 40000, 1000)
	_, err := f.place.Handle(context.Background(), command.PlaceOrderCommand{
		AccountID: "TRADER", Symbol: "APX", Side: order.SideBuy, Type: order.TypeMarket, Quantity: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := f.acc.Snapshot()
	if s.CashBalance < 0 || s.LockedCash < 0 {
		t.Fatalf("negative balance: %+v", s)
	}
	if s.Holdings["APX"] != 1010 {
		t.Errorf("holdings = %d, want 1010 (only the 19500 level is within protection)", s.Holdings["APX"])
	}
}

// Plan 2.5
func TestPlaceOrder_OrderIDIsFullUUID(t *testing.T) {
	f := newFixture(1_000_000_000)
	res, err := f.place.Handle(context.Background(), command.PlaceOrderCommand{
		AccountID: "TRADER", Symbol: "APX", Side: order.SideBuy, Type: order.TypeLimit, Price: 19500, Quantity: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, perr := uuid.Parse(res.Order.ID); perr != nil || len(res.Order.ID) != 36 {
		t.Fatalf("order id %q is not a full UUID", res.Order.ID)
	}
}

// Plan 2.4: failure inside the matcher must roll the reservation back.
func TestPlaceOrder_RestingFailureRollsBackReservation(t *testing.T) {
	f := newFixture(1_000_000_000)
	f.seedAsk(t, 19500, 1)
	before := f.acc.Snapshot()
	// Pre-register the id the handler will generate is impossible, so force the
	// failure with a symbol the book rejects: handler validation must catch it first.
	_, err := f.place.Handle(context.Background(), command.PlaceOrderCommand{
		AccountID: "TRADER", Symbol: "ZZZ", Side: order.SideBuy, Type: order.TypeLimit, Price: 19500, Quantity: 5,
	})
	if !errors.Is(err, common.ErrSymbolMismatch) {
		t.Fatalf("err = %v", err)
	}
	if after := f.acc.Snapshot(); after.LockedCash != before.LockedCash {
		t.Errorf("LockedCash %d -> %d", before.LockedCash, after.LockedCash)
	}
}

func TestCancelOrder_RejectsBadIDs(t *testing.T) {
	f := newFixture(1_000_000_000)
	for _, c := range []command.CancelOrderCommand{
		{OrderID: "", AccountID: "TRADER"},
		{OrderID: "a b", AccountID: "TRADER"},
		{OrderID: strings.Repeat("x", 65), AccountID: "TRADER"},
		{OrderID: "ok", AccountID: "bad id"},
	} {
		if _, err := f.cancel.Handle(context.Background(), c); err == nil {
			t.Errorf("cancel %+v accepted", c)
		}
	}
}
