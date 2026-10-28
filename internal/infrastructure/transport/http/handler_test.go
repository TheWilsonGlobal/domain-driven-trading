package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/query"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/auth"
	httpt "github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/transport/http"
)

type repo struct {
	accs   map[string]*account.Account
	getErr error            // when set, every GetByID fails with it
	deny   map[string]error // per-id GetByID failures
	saves  int
}

func (r *repo) GetByID(_ context.Context, id string) (*account.Account, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	if err, ok := r.deny[id]; ok {
		return nil, err
	}
	if a, ok := r.accs[id]; ok {
		return a, nil
	}
	return nil, common.ErrAccountNotFound
}
func (r *repo) Save(_ context.Context, a *account.Account) error {
	r.saves++
	r.accs[a.ID] = a
	return nil
}

type env struct {
	srv *httptest.Server
	m   *order.Matcher
	acc *account.Account
	rp  *repo
}

func newEnv(t *testing.T) *env {
	t.Helper()
	m := order.NewMatcher(order.NewOrderBook("APX"))
	acc := account.NewAccount("TRADER", 1_000_000_000)
	acc.CreditStock("APX", 100)
	rp := &repo{accs: map[string]*account.Account{"TRADER": acc, "OTHER": account.NewAccount("OTHER", 1_000_000), "FEE_COLLECTOR": account.NewAccount("FEE_COLLECTOR", 0)}}
	mw, err := auth.NewMiddleware(auth.Config{Mode: auth.ModeDisabled}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := httpt.NewHTTPHandler(
		command.NewPlaceOrderHandler(m, rp, nil),
		command.NewCancelOrderHandler(m, rp, nil),
		query.NewGetOrderBookHandler(m),
		query.NewGetBalanceHandler(rp),
		mw,
	)
	srv := httptest.NewServer(h.Router())
	t.Cleanup(srv.Close)
	return &env{srv: srv, m: m, acc: acc, rp: rp}
}

// do sends a request as account acct ("" sends no identity at all).
func (e *env) do(t *testing.T, method, path, acct, ctype, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if acct != "" {
		req.Header.Set(auth.DevAccountHeader, acct)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (e *env) post(t *testing.T, ctype, body string) *http.Response {
	t.Helper()
	return e.do(t, http.MethodPost, "/v1/orders", "TRADER", ctype, body)
}

const goodBody = `{"client_order_id":"c1","account_id":"TRADER","symbol":"APX","side":"BUY","order_type":"LIMIT","price":19500,"quantity":10}`

func TestPlaceOrder_HTTPRejections(t *testing.T) {
	tests := []struct {
		name, ctype, body string
		want              int
	}{
		{"no content type", "", goodBody, 415},
		{"text/plain", "text/plain", goodBody, 415},
		{"form", "application/x-www-form-urlencoded", goodBody, 415},
		{"body over 4KiB", "application/json", `{"client_order_id":"` + strings.Repeat("a", 5000) + `"}`, 413},
		{"unknown field", "application/json", strings.Replace(goodBody, `"quantity":10`, `"quantity":10,"is_admin":true`, 1), 400},
		{"trailing garbage", "application/json", goodBody + `{"x":1}`, 400},
		{"not json", "application/json", `nope`, 400},
		{"negative quantity", "application/json", strings.Replace(goodBody, `"quantity":10`, `"quantity":-1`, 1), 400},
		{"bad side", "application/json", strings.Replace(goodBody, `"BUY"`, `"HOLD"`, 1), 400},
		{"bad type", "application/json", strings.Replace(goodBody, `"LIMIT"`, `"STOP"`, 1), 400},
		{"zero qty", "application/json", strings.Replace(goodBody, `"quantity":10`, `"quantity":0`, 1), 400},
		{"off tick", "application/json", strings.Replace(goodBody, `19500`, `19505`, 1), 400},
		{"unknown symbol", "application/json", strings.Replace(goodBody, `"APX"`, `"ZZZ"`, 1), 400},
		{"price overflow", "application/json", strings.Replace(goodBody, `19500`, `9223372036854775807`, 1), 400},
		{"market buy no ask", "application/json", strings.Replace(strings.Replace(goodBody, `"LIMIT"`, `"MARKET"`, 1), `19500`, `0`, 1), 422},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			before := e.acc.Snapshot()
			resp := e.post(t, tc.ctype, tc.body)
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.want)
			}
			after := e.acc.Snapshot()
			if after.LockedCash != before.LockedCash || after.CashBalance != before.CashBalance ||
				after.Holdings["APX"] != before.Holdings["APX"] || after.LockedStocks["APX"] != before.LockedStocks["APX"] {
				t.Errorf("account changed: %+v -> %+v", before, after)
			}
			if d := e.m.Depth(50); len(d.Bids)+len(d.Asks) != 0 {
				t.Errorf("book changed: %+v", d)
			}
		})
	}
}

func TestPlaceOrder_HTTPSuccessShapeAndCharsetContentType(t *testing.T) {
	e := newEnv(t)
	resp := e.post(t, "application/json; charset=utf-8", goodBody)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Order   map[string]interface{}   `json:"order"`
		Matches []map[string]interface{} `json:"matches"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"order_id", "client_order_id", "account_id", "symbol", "side", "order_type", "price", "quantity", "filled_quantity", "status"} {
		if _, ok := out.Order[k]; !ok {
			t.Errorf("order missing %q", k)
		}
	}
	if e.acc.Snapshot().LockedCash <= 0 {
		t.Error("expected reservation for resting BUY")
	}
}

func TestOrderBook_HTTPLevelsAndSymbol(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 60; i++ {
		o, _ := order.NewOrder("O"+string(rune('A'+i/26))+string(rune('a'+i%26)), "", "X", "APX", order.SideBuy, order.TypeLimit, order.Price(10000+i*10), 1)
		if _, err := e.m.ProcessOrder(o); err != nil {
			t.Fatal(err)
		}
	}
	get := func(q string) (*http.Response, int) {
		resp := e.do(t, http.MethodGet, "/v1/orderbook"+q, "TRADER", "", "")
		var d struct {
			Bids []interface{} `json:"bids"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&d)
		return resp, len(d.Bids)
	}
	for q, want := range map[string]int{"": 10, "?levels=1": 1, "?levels=1000": 50, "?levels=0": 1, "?levels=-4": 1, "?symbol=APX&levels=20": 20} {
		if resp, n := get(q); resp.StatusCode != 200 || n != want {
			t.Errorf("%q: status=%d bids=%d, want %d", q, resp.StatusCode, n, want)
		}
	}
	if resp, _ := get("?levels=abc"); resp.StatusCode != 400 {
		t.Errorf("levels=abc status = %d", resp.StatusCode)
	}
	if resp, _ := get("?symbol=ZZZ"); resp.StatusCode != 404 {
		t.Errorf("unknown symbol status = %d, want 404", resp.StatusCode)
	}
}

func TestCancelAndBalance_HTTPBadIDs(t *testing.T) {
	e := newEnv(t)
	resp := e.do(t, http.MethodDelete, "/v1/orders/bad%20id", "TRADER", "", "")
	if resp.StatusCode != 400 {
		t.Errorf("cancel bad id status = %d", resp.StatusCode)
	}
	// A foreign or malformed id is a 404 on balance: the caller may only read
	// its own account, and nothing else reveals which ids exist.
	resp = e.do(t, http.MethodGet, "/v1/balance/bad%20id", "TRADER", "", "")
	if resp.StatusCode != 404 {
		t.Errorf("balance bad id status = %d, want 404", resp.StatusCode)
	}
}

// Plan 3.5: cancelling somebody else's order is indistinguishable from an
// unknown order (404); the owner can still cancel afterwards (200).
func TestCancelOrder_OwnershipStatusCodes(t *testing.T) {
	e := newEnv(t)
	resp := e.post(t, "application/json", goodBody)
	var created struct {
		Order struct {
			ID string `json:"order_id"`
		} `json:"order"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil || created.Order.ID == "" {
		t.Fatalf("place: status=%d err=%v", resp.StatusCode, err)
	}
	del := func(id, acct string) (int, string) {
		// account_id in the query is ignored; only the identity counts.
		r := e.do(t, http.MethodDelete, "/v1/orders/"+id+"?account_id=TRADER&symbol=APX", acct, "", "")
		var b map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&b)
		msg, _ := b["error"].(string)
		return r.StatusCode, msg
	}

	foreignStatus, foreignMsg := del(created.Order.ID, "OTHER")
	unknownStatus, unknownMsg := del("no-such-order", "OTHER")
	if foreignStatus != 404 || unknownStatus != 404 || foreignMsg != unknownMsg {
		t.Fatalf("foreign=%d %q unknown=%d %q; want identical 404s", foreignStatus, foreignMsg, unknownStatus, unknownMsg)
	}
	if st, _ := del(created.Order.ID, "TRADER"); st != 200 {
		t.Fatalf("owner cancel status = %d, want 200", st)
	}
	if acc := e.acc.Snapshot(); acc.LockedCash != 0 {
		t.Errorf("locked_cash = %d after owner cancel", acc.LockedCash)
	}
}
