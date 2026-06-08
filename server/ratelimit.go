package server

import (
	"net/http"
	"sync"
	"time"
)

// RateLimiter is a per-IP token bucket rate limiter.  When rate is 0,
// all requests pass through.  Thread-safe.
type RateLimiter struct {
	rate  float64 // requests per second (0 = disabled)
	burst int

	mu    sync.Mutex
	state map[string]*bucket
	once  sync.Once // cleanup goroutine gate
}

type bucket struct {
	tokens  float64
	lastRef time.Time
}

// NewRateLimiter creates a per-IP rate limiter.  rate is requests per second;
// burst is the maximum burst size.  When rate is 0, limiting is disabled
// (every request passes).  Idle entries are cleaned up every minute.
func NewRateLimiter(rate float64, burst int) *RateLimiter {
	return &RateLimiter{
		rate:  rate,
		burst: burst,
		state: make(map[string]*bucket),
	}
}

// Allow reports whether the request from the given IP should proceed.
func (rl *RateLimiter) Allow(ip string, now time.Time) bool {
	if rl.rate <= 0 {
		return true
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Lazy-start cleanup goroutine.
	rl.once.Do(func() {
		go rl.cleanupLoop()
	})

	b, ok := rl.state[ip]
	if !ok {
		b = &bucket{tokens: float64(rl.burst), lastRef: now}
		rl.state[ip] = b
	}

	// Refill tokens based on elapsed time.
	elapsed := now.Sub(b.lastRef).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastRef = now

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}
	return false
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		cutoff := time.Now().Add(-5 * time.Minute)
		for ip, b := range rl.state {
			if b.lastRef.Before(cutoff) {
				delete(rl.state, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// Middleware returns HTTP middleware that rate-limits by client IP.
// Uses X-Forwarded-For if present, otherwise RemoteAddr.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !rl.Allow(ip, time.Now()) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return fwd
	}
	return r.RemoteAddr
}

// Ensure cleanup goroutine stops in tests if needed.
var _ = time.Now
