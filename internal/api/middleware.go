package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"time"
)

// APIKeyAuth requires a matching X-API-Key header or Bearer token when key is
// non-empty. Health checks are always allowed so probes keep working.
func APIKeyAuth(key string) func(http.Handler) http.Handler {
	if key == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/health" {
				next.ServeHTTP(w, r)
				return
			}
			provided := r.Header.Get("X-API-Key")
			if provided == "" {
				if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
					provided = strings.TrimPrefix(auth, "Bearer ")
				}
			}
			if subtle.ConstantTimeCompare([]byte(provided), []byte(key)) != 1 {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimit caps expensive requests (job submission and cleanup) per client
// IP in a fixed one-minute window. A limit of zero disables limiting.
func RateLimit(perMinute int) func(http.Handler) http.Handler {
	if perMinute <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	l := &ipLimiter{
		limit:  perMinute,
		window: time.Minute,
		hits:   map[string]*window{},
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isExpensive(r) {
				next.ServeHTTP(w, r)
				return
			}
			if !l.allow(clientIP(r)) {
				w.Header().Set("Retry-After", "60")
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isExpensive(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	return r.URL.Path == "/api/scrape" || r.URL.Path == "/api/cleanup"
}

func clientIP(r *http.Request) string {
	if host, _, ok := strings.Cut(r.RemoteAddr, ":"); ok && host != "" {
		return host
	}
	return r.RemoteAddr
}

type window struct {
	start time.Time
	count int
}

type ipLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string]*window
}

func (l *ipLimiter) allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.hits) > 10_000 {
		for k, v := range l.hits {
			if now.Sub(v.start) >= l.window {
				delete(l.hits, k)
			}
		}
	}

	w, ok := l.hits[key]
	if !ok || now.Sub(w.start) >= l.window {
		l.hits[key] = &window{start: now, count: 1}
		return true
	}
	if w.count >= l.limit {
		return false
	}
	w.count++
	return true
}
