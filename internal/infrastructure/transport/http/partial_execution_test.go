package http_test

import (
	"strings"
	"testing"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/persistence/memory"
)

func askQty(e *env) uint32 {
	var n uint32
	for _, l := range e.m.Depth(10).Asks {
		n += l.TotalQuantity.Uint32()
	}
	return n
}

// F1: a failure after the order matched (maker cannot be settled) must not
// release the idempotency key: the retry would execute the order twice.
func TestIdempotency_PostMatchSettleFailureKeepsKeyAndReplays500(t *testing.T) {
	e := newIdemEnv(t, memory.NewIdempotencyStore(100))
	// TRADER rests a SELL of 10.
	r, b := postWith(t, e, "TRADER", "", `{"client_order_id":"s1","symbol":"APX","side":"SELL","order_type":"LIMIT","price":19500,"quantity":10}`)
	if r.StatusCode != 201 {
		t.Fatalf("seed sell: %d %s", r.StatusCode, b)
	}
	// The maker account can no longer be resolved after the match.
	e.rp.deny = map[string]error{"TRADER": common.ErrAccountNotFound}

	req := `{"client_order_id":"b1","symbol":"APX","side":"BUY","order_type":"LIMIT","price":19500,"quantity":4}`
	r1, b1 := postWith(t, e, "OTHER", "", req)
	if r1.StatusCode != 500 || !strings.Contains(b1, "internal error") {
		t.Fatalf("first call = %d %s, want 500 internal error", r1.StatusCode, b1)
	}
	if got := askQty(e); got != 6 {
		t.Fatalf("ask depth after first call = %d, want 6 (4 matched)", got)
	}
	r2, b2 := postWith(t, e, "OTHER", "", req)
	if r2.StatusCode != 500 || b2 != b1 {
		t.Fatalf("retry = %d %s, want the stored 500", r2.StatusCode, b2)
	}
	if r2.Header.Get("Idempotent-Replay") != "true" {
		t.Fatal("retry was re-executed instead of replayed")
	}
	if got := askQty(e); got != 6 {
		t.Fatalf("ask depth after retry = %d, want 6 (executed once)", got)
	}
}

// SEC-19: the taker's response must not expose the counterparty.
func TestPlaceOrder_ResponseDoesNotLeakMakerFields(t *testing.T) {
	e := newIdemEnv(t, memory.NewIdempotencyStore(100))
	postWith(t, e, "TRADER", "", `{"client_order_id":"s1","symbol":"APX","side":"SELL","order_type":"LIMIT","price":19500,"quantity":10}`)
	req := `{"client_order_id":"b1","symbol":"APX","side":"BUY","order_type":"LIMIT","price":19500,"quantity":4}`
	for i := 0; i < 2; i++ { // second pass is the stored replay
		r, body := postWith(t, e, "OTHER", "", req)
		if r.StatusCode != 201 {
			t.Fatalf("status %d %s", r.StatusCode, body)
		}
		for _, banned := range []string{"TRADER", "maker_account_id", "maker_order_id", "maker_remaining", "maker_unlock", "taker_unlock"} {
			if strings.Contains(body, banned) {
				t.Fatalf("pass %d: response leaks %q: %s", i, banned, body)
			}
		}
		for _, want := range []string{`"trade_id"`, `"price":19500`, `"quantity":4`, `"taker_side":"BUY"`, `"taker_remaining"`, `"timestamp"`, `"matches"`} {
			if !strings.Contains(body, want) {
				t.Fatalf("pass %d: response missing %s: %s", i, want, body)
			}
		}
	}
}
