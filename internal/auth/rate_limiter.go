package auth

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// IPRateLimiter maintains per-key token-bucket rate limiters, suitable for
// per-IP or per-peer-address rate limiting at any service boundary.
type IPRateLimiter struct {
	mu              sync.Mutex
	limiters        map[string]*rateLimiterEntry
	r               rate.Limit
	burst           int
	cleanupInterval time.Duration
	lastCleanup     time.Time
}

type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewIPRateLimiter creates a new IPRateLimiter allowing rpm requests per minute
// per key with a burst equal to burst.
func NewIPRateLimiter(rpm, burst int) *IPRateLimiter {
	if rpm <= 0 {
		rpm = 60
	}
	if burst <= 0 {
		burst = rpm
	}
	return &IPRateLimiter{
		limiters:        make(map[string]*rateLimiterEntry),
		r:               rate.Every(time.Minute / time.Duration(rpm)),
		burst:           burst,
		cleanupInterval: 5 * time.Minute,
		lastCleanup:     time.Now(),
	}
}

// Allow reports whether an event for key may happen now. It is safe for
// concurrent use. Idle limiters are pruned periodically to bound memory usage.
func (l *IPRateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.maybeCleanup()
	entry, ok := l.limiters[key]
	if !ok {
		entry = &rateLimiterEntry{limiter: rate.NewLimiter(l.r, l.burst)}
		l.limiters[key] = entry
	}
	entry.lastSeen = time.Now()
	return entry.limiter.Allow()
}

// maybeCleanup removes limiters that have been idle for longer than
// cleanupInterval. Caller must hold l.mu.
func (l *IPRateLimiter) maybeCleanup() {
	if time.Since(l.lastCleanup) < l.cleanupInterval {
		return
	}
	cutoff := time.Now().Add(-l.cleanupInterval)
	for key, entry := range l.limiters {
		if entry.lastSeen.Before(cutoff) {
			delete(l.limiters, key)
		}
	}
	l.lastCleanup = time.Now()
}
