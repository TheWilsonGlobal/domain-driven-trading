package http_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/query"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/auth"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/persistence/memory"
	httpt "github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/transport/http"
)

func newEnvCfg(t *testing.T, cfg httpt.Config) *env {
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
	).WithConfig(cfg)
	srv := httptest.NewServer(h.Router())
	t.Cleanup(srv.Close)
	return &env{srv: srv, m: m, acc: acc, rp: rp}
}

func cfgWith(mod func(*httpt.Config)) httpt.Config {
	c := httpt.DefaultConfig()
	mod(&c)
	return c
}

// raw sends a request as TRADER with arbitrary extra headers (an empty value
// removes the header).
func (e *env) raw(t *testing.T, method, path string, hdr map[string]string, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	req.Header.Set(auth.DevAccountHeader, "TRADER")
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func readAll(r *http.Response) string {
	b, _ := io.ReadAll(r.Body)
	return string(b)
}

func TestRateLimit_429AfterLimitWithRetryAfter(t *testing.T) {
	e := newEnvCfg(t, cfgWith(func(c *httpt.Config) { c.RateReadPerSec = 5; c.RateWritePerSec = 3 }))
	for i := 1; i <= 3; i++ {
		if r := e.post(t, "", goodBody); r.StatusCode == 429 {
			t.Fatalf("write %d limited too early", i)
		}
	}
	r := e.post(t, "", goodBody)
	if r.StatusCode != 429 {
		t.Fatalf("4th write status = %d, want 429", r.StatusCode)
	}
	if n, err := strconv.Atoi(r.Header.Get("Retry-After")); err != nil || n < 1 {
		t.Fatalf("Retry-After = %q, want integer >= 1", r.Header.Get("Retry-After"))
	}
	// Reads have their own, larger bucket: still open after the writes ran out.
	for i := 1; i <= 5; i++ {
		if r := e.do(t, http.MethodGet, "/v1/orderbook", "TRADER", "", ""); r.StatusCode == 429 {
			t.Fatalf("read %d limited too early", i)
		}
	}
	if r := e.do(t, http.MethodGet, "/v1/orderbook", "TRADER", "", ""); r.StatusCode != 429 {
		t.Fatalf("6th read status = %d, want 429", r.StatusCode)
	}
	// DELETE counts as a write.
	if r := e.do(t, http.MethodDelete, "/v1/orders/x", "TRADER", "", ""); r.StatusCode != 429 {
		t.Fatalf("DELETE shares the exhausted write bucket, status = %d, want 429", r.StatusCode)
	}
}

func TestRateLimit_HealthIsExempt(t *testing.T) {
	e := newEnvCfg(t, cfgWith(func(c *httpt.Config) { c.RateReadPerSec = 1; c.RateWritePerSec = 1 }))
	for i := 0; i < 30; i++ {
		if r := e.raw(t, http.MethodGet, "/health", nil, ""); r.StatusCode != 200 {
			t.Fatalf("health #%d status = %d", i, r.StatusCode)
		}
	}
}

func TestRateLimit_SpoofedForwardedForDoesNotBypass(t *testing.T) {
	e := newEnvCfg(t, cfgWith(func(c *httpt.Config) { c.RateReadPerSec = 3; c.TrustedProxy = false }))
	last := 0
	for i := 0; i < 6; i++ {
		last = e.raw(t, http.MethodGet, "/v1/orderbook", map[string]string{
			"X-Forwarded-For": "10.1.1." + strconv.Itoa(i), "X-Real-IP": "10.2.2." + strconv.Itoa(i),
		}, "").StatusCode
	}
	if last != 429 {
		t.Fatalf("6th request with rotating X-Forwarded-For = %d, want 429 (header must be ignored)", last)
	}
}

func TestRateLimit_TrustedProxyHonoursForwardedFor(t *testing.T) {
	e := newEnvCfg(t, cfgWith(func(c *httpt.Config) { c.RateReadPerSec = 2; c.TrustedProxy = true }))
	for i := 0; i < 6; i++ {
		r := e.raw(t, http.MethodGet, "/v1/orderbook", map[string]string{"X-Forwarded-For": "10.1.1." + strconv.Itoa(i)}, "")
		if r.StatusCode == 429 {
			t.Fatalf("distinct client %d limited although TRUSTED_PROXY=true", i)
		}
	}
}

func TestCORS_ExactAllowlist(t *testing.T) {
	e := newEnvCfg(t, cfgWith(func(c *httpt.Config) { c.AllowedOrigins = []string{"http://localhost:5471"} }))
	r := e.raw(t, http.MethodGet, "/health", map[string]string{"Origin": "http://localhost:5471"}, "")
	if got := r.Header.Get("Access-Control-Allow-Origin"); got != "http://localhost:5471" {
		t.Fatalf("allowed origin ACAO = %q", got)
	}
	if got := r.Header.Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("ACAC = %q, want absent (credentials off)", got)
	}
	for _, bad := range []string{"http://evil.example", "http://localhost:5471.evil.example", "http://localhost:9999", "null"} {
		r := e.raw(t, http.MethodGet, "/health", map[string]string{"Origin": bad}, "")
		if got := r.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("origin %q got ACAO %q", bad, got)
		}
	}
	// Preflight from an allowed origin.
	r = e.raw(t, http.MethodOptions, "/v1/orders", map[string]string{
		"Origin": "http://localhost:5471", "Access-Control-Request-Method": "POST",
	}, "")
	if got := r.Header.Get("Access-Control-Allow-Origin"); got != "http://localhost:5471" {
		t.Fatalf("preflight ACAO = %q", got)
	}
}

func TestConfigFromEnv(t *testing.T) {
	get := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	c, err := httpt.ConfigFromEnv(get(map[string]string{"WEB_PORT": "5471"}))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"http://localhost:5471": true, "http://127.0.0.1:5471": true}
	for _, o := range c.AllowedOrigins {
		if o == "*" {
			t.Fatal("default allowlist contains a wildcard")
		}
		delete(want, o)
	}
	if len(want) != 0 {
		t.Fatalf("default allowlist %v misses %v", c.AllowedOrigins, want)
	}
	if c.TrustedProxy {
		t.Fatal("TRUSTED_PROXY must default to false")
	}

	c, err = httpt.ConfigFromEnv(get(map[string]string{
		"CORS_ALLOWED_ORIGINS": "https://app.example.com, http://localhost:3000/", "TRUSTED_PROXY": "true",
		"RATE_LIMIT_RPS": "7", "RATE_LIMIT_WRITE_RPS": "2",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.AllowedOrigins) != 2 || c.AllowedOrigins[0] != "https://app.example.com" || c.AllowedOrigins[1] != "http://localhost:3000" {
		t.Fatalf("origins = %v", c.AllowedOrigins)
	}
	if !c.TrustedProxy || c.RateReadPerSec != 7 || c.RateWritePerSec != 2 {
		t.Fatalf("cfg = %+v", c)
	}

	for _, bad := range []map[string]string{
		{"CORS_ALLOWED_ORIGINS": "*"},
		{"CORS_ALLOWED_ORIGINS": "https://a.example,*"},
		{"CORS_ALLOWED_ORIGINS": "not-an-origin"},
		{"RATE_LIMIT_RPS": "0"},
		{"RATE_LIMIT_WRITE_RPS": "abc"},
		{"TRUSTED_PROXY": "maybe"},
	} {
		if _, err := httpt.ConfigFromEnv(get(bad)); err == nil {
			t.Errorf("ConfigFromEnv(%v) accepted bad input", bad)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	e := newEnvCfg(t, httpt.DefaultConfig())
	cases := []struct {
		name, method, path string
		hdr                map[string]string
		noStore            bool
	}{
		{"health", "GET", "/health", nil, false},
		{"v1 ok", "GET", "/v1/orderbook", nil, true},
		{"v1 error", "POST", "/v1/orders", nil, true},
		{"v1 unauthorized", "GET", "/v1/orderbook", map[string]string{auth.DevAccountHeader: ""}, true},
		{"not found", "GET", "/nope", nil, false},
		{"preflight", "OPTIONS", "/v1/orders", map[string]string{"Origin": "http://localhost:5471", "Access-Control-Request-Method": "POST"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := e.raw(t, tc.method, tc.path, tc.hdr, "")
			for k, v := range map[string]string{
				"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Referrer-Policy": "no-referrer",
			} {
				if got := r.Header.Get(k); got != v {
					t.Errorf("%s = %q, want %q (status %d)", k, got, v, r.StatusCode)
				}
			}
			if tc.noStore && r.Header.Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", r.Header.Get("Cache-Control"))
			}
		})
	}
}

func assertNoInternals(t *testing.T, body string) {
	t.Helper()
	for _, bad := range []string{"command.", "order.", "account.", "*errors", "errors.", "pgx", "main.", "goroutine", ".go:", "0x"} {
		if strings.Contains(body, bad) {
			t.Errorf("response leaks %q: %s", bad, body)
		}
	}
}

const internalErrText = "command.PlaceOrderHandler: pgx connection refused: *errors.errorString at order.Foo"

func TestErrors_UnknownErrorOnPlaceIs500Fixed(t *testing.T) {
	e := newEnvCfg(t, httpt.DefaultConfig())
	e.rp.getErr = errors.New(internalErrText)
	r := e.post(t, "application/json", goodBody)
	body := readAll(r)
	if r.StatusCode != 500 {
		t.Fatalf("status = %d, want 500 (%s)", r.StatusCode, body)
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(body), &out); err != nil || out["error"] != "internal error" || len(out) != 1 {
		t.Fatalf("body = %s, want exactly the fixed internal error", body)
	}
	assertNoInternals(t, body)
}

func TestErrors_UnknownErrorOnPlaceWithIdempotencyIs500Fixed(t *testing.T) {
	e := newIdemEnv(t, memory.NewIdempotencyStore(100))
	e.rp.getErr = errors.New(internalErrText)
	r, body := postWith(t, e, "TRADER", "", `{"client_order_id":"c9",`+buy10[1:]+`}`)
	if r.StatusCode != 500 || strings.TrimSpace(body) != `{"error":"internal error"}` {
		t.Fatalf("status=%d body=%s, want 500 fixed", r.StatusCode, body)
	}
	assertNoInternals(t, body)
}

func TestErrors_UnknownErrorOnCancelIs500Fixed(t *testing.T) {
	e := newEnvCfg(t, httpt.DefaultConfig())
	resp := e.post(t, "application/json", goodBody)
	var created struct {
		Order struct {
			ID string `json:"order_id"`
		} `json:"order"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil || created.Order.ID == "" {
		t.Fatalf("place: %d %v", resp.StatusCode, err)
	}
	e.rp.getErr = errors.New(internalErrText)
	r := e.do(t, http.MethodDelete, "/v1/orders/"+created.Order.ID+"?symbol=APX", "TRADER", "", "")
	body := readAll(r)
	if r.StatusCode != 500 || strings.TrimSpace(body) != `{"error":"internal error"}` {
		t.Fatalf("status=%d body=%s, want 500 fixed", r.StatusCode, body)
	}
	assertNoInternals(t, body)
}

func TestErrors_StatusMapping(t *testing.T) {
	e := newEnvCfg(t, httpt.DefaultConfig())
	cases := []struct {
		name, method, path, ctype, body string
		want                            int
	}{
		{"unknown order", "DELETE", "/v1/orders/no-such-order?symbol=APX", "", "", 404},
		{"insufficient balance", "POST", "/v1/orders", "application/json",
			`{"symbol":"APX","side":"BUY","order_type":"LIMIT","price":19500,"quantity":500000}`, 422},
		{"bad side", "POST", "/v1/orders", "application/json", strings.Replace(goodBody, `"BUY"`, `"X"`, 1), 400},
		{"oversize", "POST", "/v1/orders", "application/json", `{"client_order_id":"` + strings.Repeat("a", 5000) + `"}`, 413},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := e.do(t, tc.method, tc.path, "TRADER", tc.ctype, tc.body)
			body := readAll(r)
			if r.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d (%s)", r.StatusCode, tc.want, body)
			}
			assertNoInternals(t, body)
		})
	}
}
