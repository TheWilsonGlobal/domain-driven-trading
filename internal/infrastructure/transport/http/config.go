package http

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Config is the HTTP hardening configuration (plan phase 7).
type Config struct {
	// RateReadPerSec / RateWritePerSec are per-client-IP limits per second on
	// /v1; POST/PUT/PATCH/DELETE count as writes.
	RateReadPerSec  int
	RateWritePerSec int
	// TrustedProxy makes the client IP come from X-Forwarded-For / X-Real-IP.
	// Leave it false unless a proxy you control sets those headers, otherwise
	// any client can pick its own IP and dodge the limiter.
	TrustedProxy bool
	// AllowedOrigins is an exact-match list used for CORS and /ws/market.
	AllowedOrigins []string
}

const (
	defaultRateReadPerSec  = 100
	defaultRateWritePerSec = 50
	defaultWebPort         = "5471"
)

// DefaultConfig is the safe local default: localhost origins only, no proxy
// trust.
func DefaultConfig() Config {
	return Config{
		RateReadPerSec:  defaultRateReadPerSec,
		RateWritePerSec: defaultRateWritePerSec,
		AllowedOrigins:  defaultOrigins(defaultWebPort),
	}
}

func defaultOrigins(webPort string) []string {
	return []string{"http://localhost:" + webPort, "http://127.0.0.1:" + webPort}
}

// ConfigFromEnv reads CORS_ALLOWED_ORIGINS (comma list of exact origins, "*"
// is rejected), WEB_PORT (default allowlist), RATE_LIMIT_RPS,
// RATE_LIMIT_WRITE_RPS and TRUSTED_PROXY. A bad value is an error.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	cfg := DefaultConfig()

	if raw := strings.TrimSpace(getenv("CORS_ALLOWED_ORIGINS")); raw != "" {
		origins, err := ParseOrigins(raw)
		if err != nil {
			return Config{}, err
		}
		cfg.AllowedOrigins = origins
	} else if wp := strings.TrimSpace(getenv("WEB_PORT")); wp != "" {
		if n, err := strconv.Atoi(wp); err != nil || n < 1 || n > 65535 {
			return Config{}, fmt.Errorf("WEB_PORT %q is not a port", wp)
		}
		cfg.AllowedOrigins = defaultOrigins(wp)
	}

	var err error
	if cfg.RateReadPerSec, err = positiveInt(getenv, "RATE_LIMIT_RPS", cfg.RateReadPerSec); err != nil {
		return Config{}, err
	}
	if cfg.RateWritePerSec, err = positiveInt(getenv, "RATE_LIMIT_WRITE_RPS", cfg.RateWritePerSec); err != nil {
		return Config{}, err
	}

	switch v := strings.ToLower(strings.TrimSpace(getenv("TRUSTED_PROXY"))); v {
	case "", "false", "0":
	case "true", "1":
		cfg.TrustedProxy = true
	default:
		return Config{}, fmt.Errorf("TRUSTED_PROXY %q must be true or false", v)
	}
	return cfg, nil
}

func positiveInt(getenv func(string) string, name string, def int) (int, error) {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s %q must be a positive integer", name, raw)
	}
	return n, nil
}

// ParseOrigins parses a comma list of exact origins (scheme://host[:port]).
// A wildcard, a path or an unparseable entry is an error.
func ParseOrigins(raw string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		o := strings.TrimRight(strings.TrimSpace(part), "/")
		if o == "" {
			continue
		}
		u, err := url.Parse(o)
		if o == "*" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" || u.RawQuery != "" {
			return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS entry %q is not an exact origin like https://app.example.com", o)
		}
		out = append(out, o)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS has no usable origin")
	}
	return out, nil
}
