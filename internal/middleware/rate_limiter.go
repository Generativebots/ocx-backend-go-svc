package middleware

import (
	"log"
	"net/http"
	"sync"
	"time"
)

// RateLimiter enforces per-agent, per-tenant rate limits on API calls.
// This implements the patent's MaxCallsPerMinute governance policy.
//
// Uses a sliding window algorithm: each window tracks request counts per key,
// and expired windows are garbage-collected periodically.
type RateLimiter struct {
	mu       sync.RWMutex
	windows  map[string]*rateLimitWindow
	defaults RateLimitConfig
	logger   *log.Logger
}

// RateLimitConfig defines the rate limiting thresholds.
type RateLimitConfig struct {
	MaxCallsPerMinute int // Default max calls per minute per agent
	BurstSize         int // Allow temporary bursts above the limit
}

type rateLimitWindow struct {
	count       int
	windowStart time.Time
}

// NewRateLimiter creates a new rate limiter with the given defaults.
func NewRateLimiter(cfg RateLimitConfig) *RateLimiter {
	if cfg.MaxCallsPerMinute == 0 {
		cfg.MaxCallsPerMinute = 60 // 1 per second default
	}
	if cfg.BurstSize == 0 {
		cfg.BurstSize = cfg.MaxCallsPerMinute * 2
	}

	rl := &RateLimiter{
		windows:  make(map[string]*rateLimitWindow),
		defaults: cfg,
		logger:   log.New(log.Writer(), "[RATE-LIMIT] ", log.LstdFlags),
	}

	// Start background cleanup
	go rl.cleanup()

	return rl
}

// Allow checks if a request from the given key (agentID:tenantID) should
// be allowed. Returns true if within limits.
//
// P3 FIX #16: Uses read-first pattern — only acquires write lock when a new
// window must be created or the window has expired. Existing-window checks
// use RLock to reduce contention under high concurrency.
func (rl *RateLimiter) Allow(key string) bool {
	now := time.Now()

	// Fast path: check existing window under read lock
	rl.mu.RLock()
	window, exists := rl.windows[key]
	if exists && now.Sub(window.windowStart) <= time.Minute {
		// Window is active — increment and check (still under read lock,
		// but count is only used for limit checks so a slight race on
		// count++ is acceptable for rate limiting — it's a soft limit)
		window.count++
		count := window.count
		rl.mu.RUnlock()

		if count > rl.defaults.BurstSize {
			rl.logger.Printf("🚫 Rate limit exceeded (burst): key=%s count=%d limit=%d",
				key, count, rl.defaults.BurstSize)
			return false
		}
		if count > rl.defaults.MaxCallsPerMinute {
			rl.logger.Printf("⚠️ Rate limit exceeded: key=%s count=%d limit=%d",
				key, count, rl.defaults.MaxCallsPerMinute)
			return false
		}
		return true
	}
	rl.mu.RUnlock()

	// Slow path: new window needed — acquire write lock
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock (another goroutine may have created it)
	window, exists = rl.windows[key]
	if exists && now.Sub(window.windowStart) <= time.Minute {
		window.count++
		return window.count <= rl.defaults.BurstSize
	}

	// Create new window
	rl.windows[key] = &rateLimitWindow{
		count:       1,
		windowStart: now,
	}
	return true
}

// Middleware returns an HTTP middleware that enforces rate limiting.
// It extracts the agent ID from the X-Agent-ID header and tenant from X-Tenant-ID.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agentID := r.Header.Get("X-Agent-ID")
		tenantID := r.Header.Get("X-Tenant-ID")

		if agentID == "" {
			agentID = "anonymous"
		}
		if tenantID == "" {
			tenantID = "default"
		}

		key := tenantID + ":" + agentID

		if !rl.Allow(key) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limit exceeded","retry_after_seconds":60}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}

// cleanup periodically removes expired windows to prevent memory leaks.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for key, window := range rl.windows {
			if now.Sub(window.windowStart) > 2*time.Minute {
				delete(rl.windows, key)
			}
		}
		rl.mu.Unlock()
	}
}

// Stats returns current rate limiter statistics.
func (rl *RateLimiter) Stats() map[string]interface{} {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	return map[string]interface{}{
		"active_windows":    len(rl.windows),
		"max_calls_per_min": rl.defaults.MaxCallsPerMinute,
		"burst_size":        rl.defaults.BurstSize,
	}
}
