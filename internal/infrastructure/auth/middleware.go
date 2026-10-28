package auth

import (
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/identity"
	"github.com/golang-jwt/jwt/v5"
)

// DevAccountHeader carries the account id in AUTH_MODE=disabled-for-local-dev.
const DevAccountHeader = "X-Dev-Account-Id"

// allowedAlgs is the fixed signature algorithm allow-list. "none" and the
// HMAC family are never accepted, which also closes the classic
// public-key-as-HMAC-secret confusion.
var allowedAlgs = []string{"RS256", "ES256"}

// KeySource resolves a key id to a public key.
type KeySource interface {
	Key(kid string) (crypto.PublicKey, error)
}

// NewMiddleware builds the /v1 authentication middleware for cfg.
func NewMiddleware(cfg Config, logger *slog.Logger) (func(http.Handler) http.Handler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	switch cfg.Mode {
	case ModeDisabled:
		logger.Warn("AUTH_MODE=disabled-for-local-dev: /v1 is NOT authenticated; the caller account is taken from the " +
			DevAccountHeader + " header. Never use this mode outside local development.")
		return devMiddleware, nil
	case ModeJWT:
		if cfg.JWKSURL == "" || cfg.Issuer == "" || cfg.Audience == "" || cfg.AccountClaim == "" {
			return nil, errors.New("jwt auth requires JWKS URL, issuer, audience and account claim")
		}
		v := &verifier{cfg: cfg, keys: NewJWKSCache(cfg.JWKSURL, JWKSOptions{}), logger: logger}
		return v.middleware, nil
	}
	return nil, fmt.Errorf("unknown auth mode %q", cfg.Mode)
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
}

func devMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(DevAccountHeader)
		if !command.ValidID(id) {
			unauthorized(w)
			return
		}
		next.ServeHTTP(w, r.WithContext(identity.WithAccountID(r.Context(), id)))
	})
}

type verifier struct {
	cfg    Config
	keys   KeySource
	logger *slog.Logger
}

func (v *verifier) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearer(r)
		if !ok {
			unauthorized(w)
			return
		}
		id, err := v.accountFromToken(raw)
		if err != nil {
			// Detail goes to the log only; the client sees a fixed body.
			v.logger.Info("auth rejected", "reason", err.Error(), "path", r.URL.Path)
			unauthorized(w)
			return
		}
		next.ServeHTTP(w, r.WithContext(identity.WithAccountID(r.Context(), id)))
	})
}

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) <= len(p) || !strings.EqualFold(h[:len(p)], p) {
		return "", false
	}
	tok := strings.TrimSpace(h[len(p):])
	return tok, tok != ""
}

func (v *verifier) accountFromToken(raw string) (string, error) {
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, v.keyfunc,
		jwt.WithValidMethods(allowedAlgs),
		jwt.WithIssuer(v.cfg.Issuer),
		jwt.WithAudience(v.cfg.Audience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return "", err
	}
	id, ok := claimString(claims, v.cfg.AccountClaim)
	if !ok || !command.ValidID(id) {
		return "", errors.New("account claim missing or malformed")
	}
	return id, nil
}

func (v *verifier) keyfunc(t *jwt.Token) (any, error) {
	kid, _ := t.Header["kid"].(string)
	if kid == "" {
		return nil, errors.New("token has no kid")
	}
	return v.keys.Key(kid)
}

// claimString walks a dotted path through nested JSON objects.
func claimString(c jwt.MapClaims, path string) (string, bool) {
	var cur any = map[string]any(c)
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur, ok = m[part]
		if !ok {
			return "", false
		}
	}
	s, ok := cur.(string)
	return s, ok
}
