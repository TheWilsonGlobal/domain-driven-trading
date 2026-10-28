package seed_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/seed"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

type fakePlacer struct {
	cmds []command.PlaceOrderCommand
	errs map[string]error // by ClientOrderID
}

func (p *fakePlacer) Handle(_ context.Context, c command.PlaceOrderCommand) (*command.PlaceOrderResult, error) {
	p.cmds = append(p.cmds, c)
	if err := p.errs[c.ClientOrderID]; err != nil {
		return nil, err
	}
	return &command.PlaceOrderResult{}, nil
}

func specs() []seed.MakerOrder {
	return []seed.MakerOrder{
		{ClientOrderID: "SEED_BID_1", AccountID: "ACC_INST_MM", Side: order.SideBuy, Price: 19450, Quantity: 5000},
		{ClientOrderID: "SEED_ASK_1", AccountID: "ACC_INST_MM", Side: order.SideSell, Price: 19500, Quantity: 5000},
	}
}

// 5.4: a restart must not duplicate the maker liquidity.
func TestMakerOrders_SkippedWhenMakerAlreadyHasOpenOrders(t *testing.T) {
	p := &fakePlacer{}
	n, err := seed.MakerOrders(context.Background(), p, "APX", map[string]int{"ACC_INST_MM": 2}, specs(), nil)
	if err != nil || n != 0 || len(p.cmds) != 0 {
		t.Fatalf("placed=%d cmds=%d err=%v, want nothing placed", n, len(p.cmds), err)
	}
}

func TestMakerOrders_PlacesOnFirstStart(t *testing.T) {
	p := &fakePlacer{}
	n, err := seed.MakerOrders(context.Background(), p, "APX", map[string]int{}, specs(), nil)
	if err != nil || n != 2 || len(p.cmds) != 2 {
		t.Fatalf("placed=%d err=%v", n, err)
	}
	if c := p.cmds[1]; c.Symbol != "APX" || c.Type != order.TypeLimit || c.Side != order.SideSell || c.Price != 19500 {
		t.Errorf("cmd = %+v", c)
	}
}

// 5.4: every Handle error is handled: a missing maker account is skipped with a
// warning (custom SEED_ACCOUNTS), anything else aborts and is returned.
func TestMakerOrders_ErrorsAreHandled(t *testing.T) {
	missing := &fakePlacer{errs: map[string]error{"SEED_BID_1": fmt.Errorf("x: %w", common.ErrAccountNotFound)}}
	n, err := seed.MakerOrders(context.Background(), missing, "APX", nil, specs(), nil)
	if err != nil || n != 1 {
		t.Fatalf("missing maker: placed=%d err=%v, want skip and continue", n, err)
	}

	boom := errors.New("db down")
	bad := &fakePlacer{errs: map[string]error{"SEED_ASK_1": boom}}
	n, err = seed.MakerOrders(context.Background(), bad, "APX", nil, specs(), nil)
	if !errors.Is(err, boom) || n != 1 {
		t.Fatalf("placed=%d err=%v, want the error returned", n, err)
	}
}
