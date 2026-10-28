package http_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/query"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/auth"
	httpt "github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/transport/http"
	"github.com/golang-jwt/jwt/v5"
)

// jwtEnv wires the real JWT middleware to the real handlers; only the key
// server and the repository are fakes.
func jwtEnv(t *testing.T) (*env, func(account string) string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1", "use": "sig", "n": enc(key.N.Bytes()), "e": enc(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(jwks.Close)
	mw, err := auth.NewMiddleware(auth.Config{Mode: auth.ModeJWT, JWKSURL: jwks.URL, Issuer: "iss", Audience: "aud", AccountClaim: "account_id"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	m := order.NewMatcher(order.NewOrderBook("APX"))
	a := account.NewAccount("ACC_A", 1_000_000_000)
	a.CreditStock("APX", 100)
	rp := &repo{accs: map[string]*account.Account{
		"ACC_A": a, "ACC_B": account.NewAccount("ACC_B", 1_000_000), "FEE_COLLECTOR": account.NewAccount("FEE_COLLECTOR", 0),
	}}
	h := httpt.NewHTTPHandler(
		command.NewPlaceOrderHandler(m, rp, nil),
		command.NewCancelOrderHandler(m, rp, nil),
		query.NewGetOrderBookHandler(m),
		query.NewGetBalanceHandler(rp),
		mw,
	)
	srv := httptest.NewServer(h.Router())
	t.Cleanup(srv.Close)
	token := func(acct string) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"iss": "iss", "aud": "aud", "exp": time.Now().Add(time.Hour).Unix(), "account_id": acct,
		})
		tok.Header["kid"] = "k1"
		s, err := tok.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	return &env{srv: srv, m: m, acc: a, rp: rp}, token
}

func (e *env) bearer(t *testing.T, method, path, token, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestV1_RequiresBearerToken(t *testing.T) {
	e, token := jwtEnv(t)
	routes := []struct{ method, path, body string }{
		{"POST", "/v1/orders", goodBody},
		{"DELETE", "/v1/orders/abc", ""},
		{"GET", "/v1/orderbook", ""},
		{"GET", "/v1/balance/ACC_A", ""},
	}
	for _, rt := range routes {
		if code, body := e.bearer(t, rt.method, rt.path, "", rt.body); code != 401 || body != "{\"error\":\"unauthorized\"}\n" {
			t.Errorf("%s %s without token: %d %q", rt.method, rt.path, code, body)
		}
		if code, _ := e.bearer(t, rt.method, rt.path, "garbage", rt.body); code != 401 {
			t.Errorf("%s %s garbage token: %d", rt.method, rt.path, code)
		}
	}
	if code, _ := e.bearer(t, "GET", "/v1/balance/ACC_A", token("ACC_A"), ""); code != 200 {
		t.Errorf("valid token balance: %d", code)
	}
	resp, err := http.Get(e.srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("/health must stay public, got %d", resp.StatusCode)
	}
}

// A body naming another account must not change who the order is for.
func TestPlaceOrder_BodyAccountIDIsIgnored(t *testing.T) {
	e, token := jwtEnv(t)
	body := strings.Replace(goodBody, `"account_id":"TRADER"`, `"account_id":"ACC_B"`, 1)
	code, out := e.bearer(t, "POST", "/v1/orders", token("ACC_A"), body)
	if code != 201 {
		t.Fatalf("status=%d body=%s", code, out)
	}
	var res struct {
		Order struct {
			AccountID string `json:"account_id"`
		} `json:"order"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.Order.AccountID != "ACC_A" {
		t.Fatalf("order account = %q (err %v), want ACC_A", res.Order.AccountID, err)
	}
	if e.rp.accs["ACC_B"].Snapshot().LockedCash != 0 {
		t.Error("ACC_B was charged for an order it did not place")
	}
	if e.acc.Snapshot().LockedCash <= 0 {
		t.Error("ACC_A (the token account) was not charged")
	}
}

func TestBalanceAndCancel_CrossAccountIs404(t *testing.T) {
	e, token := jwtEnv(t)
	code, out := e.bearer(t, "POST", "/v1/orders", token("ACC_A"), strings.Replace(goodBody, `"TRADER"`, `"ACC_A"`, 1))
	if code != 201 {
		t.Fatalf("place: %d %s", code, out)
	}
	var res struct {
		Order struct {
			ID string `json:"order_id"`
		} `json:"order"`
	}
	_ = json.Unmarshal([]byte(out), &res)

	if code, _ := e.bearer(t, "GET", "/v1/balance/ACC_A", token("ACC_B"), ""); code != 404 {
		t.Errorf("B reading A's balance: %d, want 404", code)
	}
	// Existing and non-existing foreign ids are indistinguishable.
	c1, b1 := e.bearer(t, "GET", "/v1/balance/ACC_A", token("ACC_B"), "")
	c2, b2 := e.bearer(t, "GET", "/v1/balance/NOPE", token("ACC_B"), "")
	if c1 != c2 || b1 != b2 {
		t.Errorf("existence leak: %d %q vs %d %q", c1, b1, c2, b2)
	}
	if code, _ := e.bearer(t, "DELETE", "/v1/orders/"+res.Order.ID+"?account_id=ACC_A", token("ACC_B"), ""); code != 404 {
		t.Errorf("B cancelling A's order: %d, want 404", code)
	}
	if code, _ := e.bearer(t, "DELETE", "/v1/orders/"+res.Order.ID, token("ACC_A"), ""); code != 200 {
		t.Errorf("A cancelling own order: %d, want 200", code)
	}
}

func TestBalance_UnknownAccountIs404AndInsertsNothing(t *testing.T) {
	e, token := jwtEnv(t)
	before := len(e.rp.accs)
	code, body := e.bearer(t, "GET", "/v1/balance/NOPE", token("NOPE"), "")
	if code != 404 {
		t.Fatalf("status=%d body=%s, want 404", code, body)
	}
	if e.rp.saves != 0 || len(e.rp.accs) != before {
		t.Fatalf("read path wrote: saves=%d accounts %d -> %d", e.rp.saves, before, len(e.rp.accs))
	}
}

func TestBalance_RepositoryErrorIs500AndBalanceUnchanged(t *testing.T) {
	e, token := jwtEnv(t)
	before := e.acc.Snapshot()
	e.rp.getErr = http.ErrHandlerTimeout // any non-NotFound error
	code, body := e.bearer(t, "GET", "/v1/balance/ACC_A", token("ACC_A"), "")
	if code != 500 {
		t.Fatalf("status=%d body=%s, want 500", code, body)
	}
	if strings.Contains(body, "timeout") || strings.Contains(body, "Handler") {
		t.Errorf("internal error text leaked: %s", body)
	}
	e.rp.getErr = nil
	after := e.acc.Snapshot()
	if after.CashBalance != before.CashBalance || after.LockedCash != before.LockedCash || e.rp.saves != 0 {
		t.Errorf("balance changed after repo error: %+v -> %+v saves=%d", before, after, e.rp.saves)
	}
	_ = common.ErrAccountNotFound
}
