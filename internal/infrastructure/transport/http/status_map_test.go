package http_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// Domain rejections that the table in TestPlaceOrder_HTTPRejections does not
// reach: each maps to a fixed status and leaves no residue.
func TestPlaceOrder_DomainRejectionStatuses(t *testing.T) {
	sell := strings.Replace(goodBody, `"BUY"`, `"SELL"`, 1)
	t.Run("sell above holding is 422", func(t *testing.T) {
		e := newEnv(t) // TRADER holds 100 APX
		body := strings.Replace(sell, `"quantity":10`, `"quantity":101`, 1)
		if resp := e.post(t, "application/json", body); resp.StatusCode != 422 {
			t.Fatalf("status = %d, want 422", resp.StatusCode)
		}
		if e.acc.Snapshot().LockedStocks["APX"] != 0 {
			t.Error("shares locked by a rejected order")
		}
	})
	t.Run("cash above balance is 422", func(t *testing.T) {
		e := newEnv(t)
		body := strings.Replace(goodBody, `"quantity":10`, `"quantity":999999`, 1)
		if resp := e.post(t, "application/json", body); resp.StatusCode != 422 {
			t.Fatalf("status = %d, want 422", resp.StatusCode)
		}
		if e.acc.Snapshot().LockedCash != 0 {
			t.Error("cash locked by a rejected order")
		}
	})
}

// GET /v1/balance serves the query read model: the public JSON keys are fixed.
func TestBalance_ResponseIsTheReadModelShape(t *testing.T) {
	e := newEnv(t)
	resp := e.do(t, "GET", "/v1/balance/TRADER", "TRADER", "", "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"account_id", "cash_balance", "locked_cash", "holdings", "locked_stocks", "updated_at"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %q in %v", k, m)
		}
	}
	if m["account_id"] != "TRADER" {
		t.Errorf("account_id = %v", m["account_id"])
	}
}
