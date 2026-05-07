package auth

import (
	"sync"
	"time"
)

const (
	maxVerifyAttempts   = 5
	maxRegisterAttempts = 3
)

type rlEntry struct {
	count       int
	windowStart time.Time
}

type verifyRateLimiter struct {
	mu       sync.Mutex
	entries  map[string]*rlEntry
	window   time.Duration
	max      int
	done     chan struct{}
	stopOnce sync.Once
}

func newRateLimiter(max int) *verifyRateLimiter {
	rl := &verifyRateLimiter{
		entries: make(map[string]*rlEntry),
		window:  codeExpiry,
		max:     max,
		done:    make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// Stop signals the cleanup goroutine to exit. Safe to call multiple times.
func (r *verifyRateLimiter) Stop() {
	r.stopOnce.Do(func() { close(r.done) })
}

// cleanupLoop runs every window duration and evicts expired entries to prevent unbounded memory growth.
func (r *verifyRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(r.window)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.mu.Lock()
			now := time.Now()
			for email, e := range r.entries {
				if now.Sub(e.windowStart) >= r.window {
					delete(r.entries, email)
				}
			}
			r.mu.Unlock()
		case <-r.done:
			return
		}
	}
}

// allow returns true if the attempt is within the limit, false if the caller is rate-limited.
func (r *verifyRateLimiter) allow(email string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	e, ok := r.entries[email]
	if !ok || now.Sub(e.windowStart) >= r.window {
		r.entries[email] = &rlEntry{count: 1, windowStart: now}
		return true
	}
	if e.count >= r.max {
		return false
	}
	e.count++
	return true
}

// reset clears the attempt counter for an email (called after a new code is issued or on success).
func (r *verifyRateLimiter) reset(email string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, email)
}
