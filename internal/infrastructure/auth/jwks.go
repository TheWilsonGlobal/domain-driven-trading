package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultMinRefresh = time.Minute
	maxJWKSBytes      = 1 << 20
)

// JWKSOptions tunes a JWKSCache; zero values select production defaults.
type JWKSOptions struct {
	HTTPClient *http.Client
	// MinRefresh is the minimum gap between two fetches of the JWKS URL, so a
	// stream of tokens with unknown kids cannot turn into a fetch storm.
	MinRefresh time.Duration
	Now        func() time.Time
}

// JWKSCache holds the public keys published at a JWKS URL. Keys are cached;
// an unknown kid triggers a refresh, at most once per MinRefresh. A failed
// refresh keeps the previous keys.
type JWKSCache struct {
	url        string
	client     *http.Client
	minRefresh time.Duration
	now        func() time.Time

	fetchMu     sync.Mutex // serialises fetches
	lastAttempt time.Time

	mu   sync.RWMutex
	keys map[string]crypto.PublicKey
}

func NewJWKSCache(url string, opts JWKSOptions) *JWKSCache {
	c := &JWKSCache{url: url, client: opts.HTTPClient, minRefresh: opts.MinRefresh, now: opts.Now}
	if c.client == nil {
		c.client = &http.Client{Timeout: 5 * time.Second, CheckRedirect: JWKSCheckRedirect}
	}
	if c.minRefresh <= 0 {
		c.minRefresh = defaultMinRefresh
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c
}

var errKeyNotFound = errors.New("jwks: key not found")

// Key returns the public key for kid, refreshing the set when kid is unknown
// and the rate limit allows.
func (c *JWKSCache) Key(kid string) (crypto.PublicKey, error) {
	if k, ok := c.lookup(kid); ok {
		return k, nil
	}
	if err := c.refresh(kid); err != nil {
		return nil, err
	}
	if k, ok := c.lookup(kid); ok {
		return k, nil
	}
	return nil, errKeyNotFound
}

func (c *JWKSCache) lookup(kid string) (crypto.PublicKey, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	k, ok := c.keys[kid]
	return k, ok
}

func (c *JWKSCache) refresh(kid string) error {
	c.fetchMu.Lock()
	defer c.fetchMu.Unlock()
	// Another goroutine may have refreshed while we waited.
	if _, ok := c.lookup(kid); ok {
		return nil
	}
	now := c.now()
	if !c.lastAttempt.IsZero() && now.Sub(c.lastAttempt) < c.minRefresh {
		return nil // rate limited: caller reports key not found
	}
	c.lastAttempt = now
	keys, err := c.fetch()
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.keys = keys
	c.mu.Unlock()
	return nil
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func (c *JWKSCache) fetch() (map[string]crypto.PublicKey, error) {
	resp, err := c.client.Get(c.url)
	if err != nil {
		return nil, fmt.Errorf("jwks fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks fetch: status %d", resp.StatusCode)
	}
	var doc struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJWKSBytes)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("jwks decode: %w", err)
	}
	out := make(map[string]crypto.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kid == "" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		pub, err := k.public()
		if err != nil {
			continue // skip unusable keys, keep the rest
		}
		out[k.Kid] = pub
	}
	return out, nil
}

func (k jwk) public() (crypto.PublicKey, error) {
	dec := func(s string) (*big.Int, error) {
		b, err := base64.RawURLEncoding.DecodeString(s)
		if err != nil || len(b) == 0 {
			return nil, errors.New("bad base64")
		}
		return new(big.Int).SetBytes(b), nil
	}
	switch k.Kty {
	case "RSA":
		n, err := dec(k.N)
		if err != nil {
			return nil, err
		}
		e, err := dec(k.E)
		if err != nil || !e.IsInt64() || e.Int64() < 3 || e.Int64() > 1<<31-1 {
			return nil, errors.New("bad rsa exponent")
		}
		if n.BitLen() < 2048 {
			return nil, errors.New("rsa key too small")
		}
		return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
	case "EC":
		if k.Crv != "P-256" {
			return nil, errors.New("unsupported curve")
		}
		x, err := dec(k.X)
		if err != nil {
			return nil, err
		}
		y, err := dec(k.Y)
		if err != nil {
			return nil, err
		}
		pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
		if !pub.Curve.IsOnCurve(x, y) {
			return nil, errors.New("point not on curve")
		}
		return pub, nil
	}
	return nil, errors.New("unsupported kty")
}

// IsLoopbackHost reports whether host (no port) is localhost or a loopback IP.
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// JWKSCheckRedirect is the http.Client CheckRedirect of the JWKS fetcher: a
// redirect may only lead to https (or loopback, for local development), so a
// compromised or misconfigured endpoint cannot downgrade the key fetch to
// plaintext. It also caps the chain at 3 hops.
func JWKSCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 {
		return errors.New("jwks: too many redirects")
	}
	if req.URL.Scheme != "https" && !IsLoopbackHost(req.URL.Hostname()) {
		return errors.New("jwks: refusing redirect to non-https URL")
	}
	return nil
}
