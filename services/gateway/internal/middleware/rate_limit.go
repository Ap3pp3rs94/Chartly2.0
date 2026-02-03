package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type bucket struct {
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
}
type limiter struct {
	mu       sync.Mutex
	ratePerS float64
	burst    float64
	buckets  map[string]*bucket
}

func newLimiter(rpm int, burst int) *limiter {
	if rpm < 1 {
		rpm = 600
	}
	if burst < 1 {
		burst = 100
	}
	l := &limiter{
		ratePerS: float64(rpm) / 60.0,
		burst:    float64(burst),
		buckets:  make(map[string]*bucket),
	}
	go l.cleanupLoop()
	return l
}
func (l *limiter) cleanupLoop() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().UTC().Add(-15 * time.Minute)
		l.mu.Lock()
		for k, b := range l.buckets {
			if b.lastSeen.Before(cutoff) {
				delete(l.buckets, k)
			}
		}
		l.mu.Unlock()
	}
}
func (l *limiter) allow(key string) (allowed bool, retryAfter time.Duration) {
	now := time.Now().UTC()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{
			tokens:     l.burst,
			lastRefill: now,
			lastSeen:   now,
		}
		l.buckets[key] = b
	}

	// refill tokens
	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed > 0 {
		b.tokens = min(l.burst, b.tokens+(elapsed*l.ratePerS))
		b.lastRefill = now
	}
	b.lastSeen = now

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true, 0
	}

	// compute retry-after (time to reach 1 token)
	need := 1.0 - b.tokens
	secs := need / l.ratePerS
	if secs < 0 {
		secs = 0
	}
	return false, time.Duration(secs * float64(time.Second))
}
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func parseIntEnv(name string, def int) int {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// Hash IP in-memory keying to avoid persisting raw IP as map keys in memory dumps/logs.
// This is not a cryptographic privacy guarantee, but reduces accidental exposure.
func ipKey(ip string) string {
	sum := sha256.Sum256([]byte(ip))
	return hex.EncodeToString(sum[:16]) // 128-bit truncated
}
func clientIP(r *http.Request) string {
	// Prefer X-Forwarded-For first IP (common in proxied environments).
	xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}

	// Fallback to RemoteAddr
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

var globalLimiter = newLimiter(
	parseIntEnv("GATEWAY_RL_RPM", 600),
	parseIntEnv("GATEWAY_RL_BURST", 100),
)

func RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		key := ipKey(ip)
		ok, retry := globalLimiter.allow(key)
		if ok {
			next.ServeHTTP(w, r)
			// return
		}

		// Retry-After best-effort (integer seconds)
		ra := int(retry.Seconds())
		if ra < 1 {
			ra = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(ra))
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
	})
}
