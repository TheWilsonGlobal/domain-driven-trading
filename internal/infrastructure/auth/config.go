// Package auth authenticates HTTP callers. It lives in infrastructure; the app
// and domain layers only see the account id via app/identity.
package auth

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/domain-driven-trading/domain-driven-trading/internal/config"
)

type Mode string

const (
	ModeJWT      Mode = "jwt"
	ModeDisabled Mode = "disabled-for-local-dev"
)

// Config is the authentication configuration read from the environment.
type Config struct {
	Mode         Mode
	JWKSURL      string
	Issuer       string
	Audience     string
	AccountClaim string
}

// LoadConfig reads AUTH_MODE (default jwt), JWKS_URL, JWT_ISSUER, JWT_AUDIENCE
// and AUTH_ACCOUNT_CLAIM. It fails closed: an unknown mode, or jwt mode with
// any required value missing, is an error so the process refuses to start.
func LoadConfig(getenv func(string) string) (Config, error) {
	cfg := Config{
		Mode:         Mode(strings.TrimSpace(getenv("AUTH_MODE"))),
		JWKSURL:      strings.TrimSpace(getenv("JWKS_URL")),
		Issuer:       strings.TrimSpace(getenv("JWT_ISSUER")),
		Audience:     strings.TrimSpace(getenv("JWT_AUDIENCE")),
		AccountClaim: strings.TrimSpace(getenv("AUTH_ACCOUNT_CLAIM")),
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeJWT
	}
	if cfg.AccountClaim == "" {
		cfg.AccountClaim = config.DefaultAccountClaim
	}
	switch cfg.Mode {
	case ModeDisabled:
		// The unauthenticated mode trusts a client-supplied header, so it needs
		// a second, explicit switch and may not coexist with a real JWKS_URL
		// (a production config copied next to a dev flag must not fail open).
		if strings.TrimSpace(getenv("ALLOW_INSECURE_DEV_AUTH")) != "1" {
			return Config{}, fmt.Errorf("AUTH_MODE=%s also requires ALLOW_INSECURE_DEV_AUTH=1 (local development only)", ModeDisabled)
		}
		if cfg.JWKSURL != "" {
			return Config{}, fmt.Errorf("AUTH_MODE=%s must not be combined with JWKS_URL; unset JWKS_URL or use AUTH_MODE=jwt", ModeDisabled)
		}
		return cfg, nil
	case ModeJWT:
		var missing []string
		if cfg.JWKSURL == "" {
			missing = append(missing, "JWKS_URL")
		}
		if cfg.Issuer == "" {
			missing = append(missing, "JWT_ISSUER")
		}
		if cfg.Audience == "" {
			missing = append(missing, "JWT_AUDIENCE")
		}
		if len(missing) > 0 {
			return Config{}, fmt.Errorf("AUTH_MODE=jwt requires %s", strings.Join(missing, ", "))
		}
		u, err := url.Parse(cfg.JWKSURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return Config{}, fmt.Errorf("JWKS_URL must be an http(s) URL")
		}
		if u.Scheme != "https" && !IsLoopbackHost(u.Hostname()) {
			return Config{}, fmt.Errorf("JWKS_URL must be https unless the host is loopback (localhost, 127.0.0.1, ::1)")
		}
		return cfg, nil
	default:
		return Config{}, fmt.Errorf("unknown AUTH_MODE %q (want %q or %q)", cfg.Mode, ModeJWT, ModeDisabled)
	}
}
