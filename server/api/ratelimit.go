package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// ipLimiter keeps a token-bucket rate.Limiter per client IP. Unused entries
// are evicted by a background goroutine so long-running servers don't leak.
type ipLimiter struct {
	mu   sync.Mutex
	by   map[string]*bucket
	rate rate.Limit
	b    int
	ttl  time.Duration
}

type bucket struct {
	lim  *rate.Limiter
	seen time.Time
}

// newIPLimiter builds a limiter that allows `perMinute` requests per IP,
// with a small burst. Entries not hit for `ttl` are pruned.
func newIPLimiter(perMinute int, burst int, ttl time.Duration) *ipLimiter {
	rl := &ipLimiter{
		by:   map[string]*bucket{},
		rate: rate.Limit(float64(perMinute) / 60.0),
		b:    burst,
		ttl:  ttl,
	}
	go rl.gc()
	return rl
}

func (rl *ipLimiter) allow(key string) bool {
	rl.mu.Lock()
	b, ok := rl.by[key]
	if !ok {
		b = &bucket{lim: rate.NewLimiter(rl.rate, rl.b)}
		rl.by[key] = b
	}
	b.seen = time.Now()
	rl.mu.Unlock()
	return b.lim.Allow()
}

func (rl *ipLimiter) gc() {
	t := time.NewTicker(rl.ttl)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().Add(-rl.ttl)
		rl.mu.Lock()
		for k, b := range rl.by {
			if b.seen.Before(cutoff) {
				delete(rl.by, k)
			}
		}
		rl.mu.Unlock()
	}
}

// middleware wraps a handler with per-IP rate limiting. Over-quota clients
// get 429 with Retry-After.
func (rl *ipLimiter) middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !rl.allow(clientIP(r)) {
			w.Header().Set("Retry-After", "60")
			writeJSON(w, http.StatusTooManyRequests, errorResponse{"too many attempts; try again later"})
			return
		}
		next(w, r)
	}
}

// clientIP prefers X-Forwarded-For (first hop) when running behind a proxy,
// otherwise falls back to RemoteAddr without port.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
