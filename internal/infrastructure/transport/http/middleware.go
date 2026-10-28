package http

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// SecurityHeaders sets the baseline response hardening headers on every
// response it wraps.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// noStore keeps authenticated API responses out of shared caches.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// ipLimiter is a small fixed-window per-key request limiter. It exists instead
// of github.com/go-chi/httprate because every httprate release reachable from
// here requires Go >= 1.23 while this module is on 1.22.5 (the Go upgrade is
// phase 8).
type ipLimiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	nextGC  time.Time
}

type bucket struct {
	start time.Time
	count int
}

const limiterMaxKeysBeforeGC = 4096

func newIPLimiter(limit int, window time.Duration) *ipLimiter {
	return &ipLimiter{limit: limit, window: window, now: time.Now, buckets: map[string]*bucket{}}
}

// allow reports whether the key may proceed; when not, retryAfter is the time
// until its window resets.
func (l *ipLimiter) allow(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.buckets) > limiterMaxKeysBeforeGC && now.After(l.nextGC) {
		for k, b := range l.buckets {
			if now.Sub(b.start) >= l.window {
				delete(l.buckets, k)
			}
		}
		l.nextGC = now.Add(l.window)
	}
	b := l.buckets[key]
	if b == nil || now.Sub(b.start) >= l.window {
		b = &bucket{start: now}
		l.buckets[key] = b
	}
	if b.count >= l.limit {
		return false, b.start.Add(l.window).Sub(now)
	}
	b.count++
	return true, 0
}

// clientIP is the connection's peer address. Forwarding headers are only
// honoured through middleware.RealIP when TRUSTED_PROXY=true, which rewrites
// RemoteAddr before this runs.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func isWrite(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// rateLimit limits per client IP, with a stricter bucket for writes.
func rateLimit(readPerSec, writePerSec int) func(http.Handler) http.Handler {
	reads := newIPLimiter(readPerSec, time.Second)
	writes := newIPLimiter(writePerSec, time.Second)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			l := reads
			if isWrite(r.Method) {
				l = writes
			}
			if ok, wait := l.allow(clientIP(r)); !ok {
				secs := int((wait + time.Second - 1) / time.Second)
				if secs < 1 {
					secs = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				writeErr(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
