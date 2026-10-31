// Package config holds the settings that are the same in every environment.
// Secrets and per-environment endpoints stay in environment variables. Each
// value here is the default for a variable that can still override it.
package config

const (
	// DefaultPort is the gateway HTTP/WS port when PORT is unset.
	DefaultPort = "5470"
	// DefaultWebPort is the web dev server port (WEB_PORT). It builds the
	// default CORS origins when CORS_ALLOWED_ORIGINS is unset.
	DefaultWebPort = "5471"
	// DefaultRateReadPerSec and DefaultRateWritePerSec are the per-client-IP
	// limits per second on /v1 (RATE_LIMIT_RPS, RATE_LIMIT_WRITE_RPS).
	DefaultRateReadPerSec  = 100
	DefaultRateWritePerSec = 50
	// DefaultWSMaxClients caps concurrent /ws/market clients (WS_MAX_CLIENTS).
	DefaultWSMaxClients = 1000
	// DefaultAccountClaim is the token claim holding the trading account id
	// (AUTH_ACCOUNT_CLAIM). There is deliberately no fallback to "sub": a
	// Supabase "sub" is a user UUID, not a trading account, so silently using
	// it would mask a misconfiguration. A dotted path (for example
	// app_metadata.account_id) selects a nested claim.
	DefaultAccountClaim = "account_id"
)
