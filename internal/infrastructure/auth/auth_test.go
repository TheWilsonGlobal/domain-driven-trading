package auth_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/identity"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/auth"
	"github.com/golang-jwt/jwt/v5"
)

const (
	testIss = "https://issuer.test/auth/v1"
	testAud = "trading-api"
)

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

type keys struct {
	rsaKey *rsa.PrivateKey
	ecKey  *ecdsa.PrivateKey
}

func newKeys(t *testing.T) *keys {
	t.Helper()
	r, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	e, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &keys{rsaKey: r, ecKey: e}
}

func (k *keys) jwks(rsaKid, ecKid string) map[string]any {
	return map[string]any{"keys": []map[string]string{
		{"kty": "RSA", "kid": rsaKid, "alg": "RS256", "use": "sig",
			"n": b64(k.rsaKey.N.Bytes()), "e": b64(big.NewInt(int64(k.rsaKey.E)).Bytes())},
		{"kty": "EC", "kid": ecKid, "alg": "ES256", "use": "sig", "crv": "P-256",
			"x": b64(k.ecKey.X.FillBytes(make([]byte, 32))), "y": b64(k.ecKey.Y.FillBytes(make([]byte, 32)))},
	}}
}

func jwksServer(t *testing.T, doc *atomic.Value, hits *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(hits, 1)
		_ = json.NewEncoder(w).Encode(doc.Load())
	}))
	t.Cleanup(srv.Close)
	return srv
}

func claims(mut func(jwt.MapClaims)) jwt.MapClaims {
	c := jwt.MapClaims{
		"iss": testIss, "aud": testAud, "sub": "user-uuid",
		"exp": time.Now().Add(time.Hour).Unix(), "account_id": "ACC_A",
	}
	if mut != nil {
		mut(c)
	}
	return c
}

func sign(t *testing.T, m jwt.SigningMethod, key any, kid string, c jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(m, c)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type fixture struct {
	k    *keys
	hits *int32
	doc  *atomic.Value
	h    http.Handler
}

func echo(w http.ResponseWriter, r *http.Request) {
	id, _ := identity.AccountID(r.Context())
	_, _ = w.Write([]byte(id))
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	k := newKeys(t)
	doc := &atomic.Value{}
	doc.Store(k.jwks("rsa-1", "ec-1"))
	var hits int32
	srv := jwksServer(t, doc, &hits)
	mw, err := auth.NewMiddleware(auth.Config{
		Mode: auth.ModeJWT, JWKSURL: srv.URL, Issuer: testIss, Audience: testAud, AccountClaim: "account_id",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{k: k, hits: &hits, doc: doc, h: mw(http.HandlerFunc(echo))}
}

func (f *fixture) call(authz string) (int, string) {
	req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestJWT_Rejections(t *testing.T) {
	f := newFixture(t)
	hsSecret := b64(f.k.rsaKey.N.Bytes())
	rs := func(mut func(jwt.MapClaims)) string {
		return "Bearer " + sign(t, jwt.SigningMethodRS256, f.k.rsaKey, "rsa-1", claims(mut))
	}
	cases := map[string]string{
		"no token":      "",
		"not bearer":    "Basic abc",
		"garbage":       "Bearer not.a.jwt",
		"empty bearer":  "Bearer ",
		"expired":       rs(func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }),
		"no exp":        rs(func(c jwt.MapClaims) { delete(c, "exp") }),
		"nbf future":    rs(func(c jwt.MapClaims) { c["nbf"] = time.Now().Add(time.Hour).Unix() }),
		"wrong aud":     rs(func(c jwt.MapClaims) { c["aud"] = "other" }),
		"wrong iss":     rs(func(c jwt.MapClaims) { c["iss"] = "https://evil" }),
		"unknown kid":   "Bearer " + sign(t, jwt.SigningMethodRS256, f.k.rsaKey, "nope", claims(nil)),
		"bad signature": "Bearer " + sign(t, jwt.SigningMethodRS256, newKeys(t).rsaKey, "rsa-1", claims(nil)),
		"alg none":      "Bearer " + sign(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, "rsa-1", claims(nil)),
		"HS256 pubkey":  "Bearer " + sign(t, jwt.SigningMethodHS256, []byte(hsSecret), "rsa-1", claims(nil)),
		"missing claim": rs(func(c jwt.MapClaims) { delete(c, "account_id") }),
		"claim not str": rs(func(c jwt.MapClaims) { c["account_id"] = 7 }),
		"claim bad fmt": rs(func(c jwt.MapClaims) { c["account_id"] = "a b;drop" }),
	}
	for name, authz := range cases {
		t.Run(name, func(t *testing.T) {
			code, body := f.call(authz)
			if code != 401 {
				t.Fatalf("status = %d, want 401", code)
			}
			if body != "{\"error\":\"unauthorized\"}\n" {
				t.Fatalf("body = %q", body)
			}
		})
	}
}

func TestJWT_ValidTokens(t *testing.T) {
	f := newFixture(t)
	for name, authz := range map[string]string{
		"RS256": "Bearer " + sign(t, jwt.SigningMethodRS256, f.k.rsaKey, "rsa-1", claims(nil)),
		"ES256": "Bearer " + sign(t, jwt.SigningMethodES256, f.k.ecKey, "ec-1", claims(nil)),
		"aud list": "Bearer " + sign(t, jwt.SigningMethodRS256, f.k.rsaKey, "rsa-1", claims(func(c jwt.MapClaims) {
			c["aud"] = []string{"x", testAud}
		})),
	} {
		code, body := f.call(authz)
		if code != 200 || body != "ACC_A" {
			t.Errorf("%s: status=%d body=%q, want 200 ACC_A", name, code, body)
		}
	}
}

func TestJWT_NestedClaimPath(t *testing.T) {
	k := newKeys(t)
	doc := &atomic.Value{}
	doc.Store(k.jwks("rsa-1", "ec-1"))
	var hits int32
	srv := jwksServer(t, doc, &hits)
	mw, err := auth.NewMiddleware(auth.Config{Mode: auth.ModeJWT, JWKSURL: srv.URL, Issuer: testIss, Audience: testAud, AccountClaim: "app_metadata.account_id"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := mw(http.HandlerFunc(echo))
	tok := sign(t, jwt.SigningMethodRS256, k.rsaKey, "rsa-1", claims(func(c jwt.MapClaims) {
		c["app_metadata"] = map[string]any{"account_id": "ACC_NESTED"}
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "ACC_NESTED" {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestJWKS_CachesAndRateLimitsRefresh(t *testing.T) {
	k := newKeys(t)
	doc := &atomic.Value{}
	doc.Store(k.jwks("rsa-1", "ec-1"))
	var hits int32
	srv := jwksServer(t, doc, &hits)
	now := time.Now()
	c := auth.NewJWKSCache(srv.URL, auth.JWKSOptions{MinRefresh: time.Minute, Now: func() time.Time { return now }})

	if _, err := c.Key("rsa-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Key("ec-1"); err != nil {
		t.Fatal(err)
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Fatalf("known kids fetched %d times, want 1 (cached)", h)
	}
	// Unknown kid hammering must not hit the endpoint more than once per window.
	for i := 0; i < 20; i++ {
		if _, err := c.Key("ghost"); err == nil {
			t.Fatal("unknown kid resolved")
		}
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Fatalf("unknown kid refreshed within window: hits=%d, want 1", h)
	}
	// Rotation: new kid appears; after the window a refresh picks it up.
	doc.Store(k.jwks("rsa-2", "ec-1"))
	if _, err := c.Key("rsa-2"); err == nil {
		t.Fatal("refresh happened inside rate-limit window")
	}
	now = now.Add(61 * time.Second)
	if _, err := c.Key("rsa-2"); err != nil {
		t.Fatalf("rotated key not found after window: %v", err)
	}
	if h := atomic.LoadInt32(&hits); h != 2 {
		t.Fatalf("hits=%d, want 2", h)
	}
}

func TestJWKS_FetchFailureKeepsOldKeys(t *testing.T) {
	k := newKeys(t)
	doc := &atomic.Value{}
	doc.Store(k.jwks("rsa-1", "ec-1"))
	var broken atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if broken.Load() {
			http.Error(w, "boom", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(doc.Load())
	}))
	defer srv.Close()
	now := time.Now()
	c := auth.NewJWKSCache(srv.URL, auth.JWKSOptions{MinRefresh: time.Second, Now: func() time.Time { return now }})
	if _, err := c.Key("rsa-1"); err != nil {
		t.Fatal(err)
	}
	broken.Store(true)
	now = now.Add(time.Minute)
	if _, err := c.Key("ghost"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := c.Key("rsa-1"); err != nil {
		t.Fatalf("cached key lost after failed refresh: %v", err)
	}
}

func TestDisabledMode_HeaderOnly(t *testing.T) {
	mw, err := auth.NewMiddleware(auth.Config{Mode: auth.ModeDisabled}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := mw(http.HandlerFunc(echo))
	do := func(hdr, target string) (int, string) {
		req := httptest.NewRequest(http.MethodPost, target, nil)
		if hdr != "" {
			req.Header.Set("X-Dev-Account-Id", hdr)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	if c, b := do("DEV_1", "/v1/x"); c != 200 || b != "DEV_1" {
		t.Errorf("header: %d %q", c, b)
	}
	if c, _ := do("", "/v1/x?account_id=EVIL"); c != 401 {
		t.Errorf("query account_id must not authenticate, got %d", c)
	}
	if c, _ := do("bad id!", "/v1/x"); c != 401 {
		t.Errorf("malformed header id: %d", c)
	}
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadConfig(t *testing.T) {
	full := map[string]string{"JWKS_URL": "https://x/.well-known/jwks.json", "JWT_ISSUER": testIss, "JWT_AUDIENCE": testAud}
	without := func(k string) map[string]string {
		m := map[string]string{}
		for a, b := range full {
			m[a] = b
		}
		delete(m, k)
		return m
	}
	badScheme := without("JWKS_URL")
	badScheme["JWKS_URL"] = "file:///etc/passwd"
	for name, m := range map[string]map[string]string{
		"jwt default, missing JWKS_URL":     without("JWKS_URL"),
		"jwt default, missing JWT_ISSUER":   without("JWT_ISSUER"),
		"jwt default, missing JWT_AUDIENCE": without("JWT_AUDIENCE"),
		"explicit jwt, nothing set":         {"AUTH_MODE": "jwt"},
		"unknown mode":                      {"AUTH_MODE": "none"},
		"unknown mode casing":               {"AUTH_MODE": "JWT"},
		"bad JWKS scheme":                   badScheme,
	} {
		if _, err := auth.LoadConfig(env(m)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	cfg, err := auth.LoadConfig(env(full))
	if err != nil || cfg.Mode != auth.ModeJWT || cfg.AccountClaim != "account_id" {
		t.Errorf("default jwt: %+v %v", cfg, err)
	}
	cfg, err = auth.LoadConfig(env(map[string]string{"AUTH_MODE": "disabled-for-local-dev", "ALLOW_INSECURE_DEV_AUTH": "1"}))
	if err != nil || cfg.Mode != auth.ModeDisabled {
		t.Errorf("disabled: %+v %v", cfg, err)
	}
	withClaim := without("")
	withClaim["AUTH_ACCOUNT_CLAIM"] = "sub"
	cfg, err = auth.LoadConfig(env(withClaim))
	if err != nil || cfg.AccountClaim != "sub" {
		t.Errorf("claim override: %+v %v", cfg, err)
	}
}

// SEC-20: the unauthenticated mode needs an explicit second switch and must not
// coexist with a JWKS_URL.
func TestLoadConfig_DisabledModeNeedsExplicitOptInAndNoJWKS(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"no opt-in":        {"AUTH_MODE": "disabled-for-local-dev"},
		"opt-in = 0":       {"AUTH_MODE": "disabled-for-local-dev", "ALLOW_INSECURE_DEV_AUTH": "0"},
		"opt-in = true":    {"AUTH_MODE": "disabled-for-local-dev", "ALLOW_INSECURE_DEV_AUTH": "true"},
		"opt-in + JWKSURL": {"AUTH_MODE": "disabled-for-local-dev", "ALLOW_INSECURE_DEV_AUTH": "1", "JWKS_URL": "https://x/jwks"},
	} {
		if _, err := auth.LoadConfig(env(m)); err == nil {
			t.Errorf("%s: expected startup error", name)
		}
	}
	// The error names the switch so the operator knows what to do.
	_, err := auth.LoadConfig(env(map[string]string{"AUTH_MODE": "disabled-for-local-dev"}))
	if err == nil || !strings.Contains(err.Error(), "ALLOW_INSECURE_DEV_AUTH") {
		t.Errorf("error should name ALLOW_INSECURE_DEV_AUTH, got %v", err)
	}
}

// SEC-24a: JWKS_URL must be https unless it points at loopback.
func TestLoadConfig_JWKSURLMustBeHTTPSUnlessLoopback(t *testing.T) {
	base := func(u string) map[string]string {
		return map[string]string{"JWKS_URL": u, "JWT_ISSUER": testIss, "JWT_AUDIENCE": testAud}
	}
	for _, u := range []string{"http://auth.example.com/jwks", "http://10.0.0.5/jwks", "http://localhost.evil.com/jwks"} {
		if _, err := auth.LoadConfig(env(base(u))); err == nil {
			t.Errorf("%s: expected rejection", u)
		}
	}
	for _, u := range []string{"https://auth.example.com/jwks", "http://localhost:9999/jwks", "http://127.0.0.1:9999/jwks", "http://[::1]:9999/jwks"} {
		if _, err := auth.LoadConfig(env(base(u))); err != nil {
			t.Errorf("%s: unexpected error %v", u, err)
		}
	}
}

func TestJWKSCheckRedirect_RefusesNonHTTPS(t *testing.T) {
	mk := func(u string) *http.Request { r, _ := http.NewRequest(http.MethodGet, u, nil); return r }
	if err := auth.JWKSCheckRedirect(mk("http://evil.example.com/jwks"), nil); err == nil {
		t.Error("redirect to http://evil.example.com accepted")
	}
	if err := auth.JWKSCheckRedirect(mk("https://auth.example.com/jwks"), nil); err != nil {
		t.Errorf("https redirect refused: %v", err)
	}
	if err := auth.JWKSCheckRedirect(mk("http://127.0.0.1:1/jwks"), nil); err != nil {
		t.Errorf("loopback redirect refused: %v", err)
	}
	via := make([]*http.Request, 3)
	if err := auth.JWKSCheckRedirect(mk("https://a.example.com/"), via); err == nil {
		t.Error("redirect chain over the limit accepted")
	}
}
