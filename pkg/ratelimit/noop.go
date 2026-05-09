package ratelimit

import "context"

// NoopLimiter always allows. Use it as a stand-in for a real limiter when the
// underlying backing store (Redis) is unavailable, so handlers that depend on
// a non-nil RateLimiter remain functional rather than degrading to a 503.
//
// This intentionally trades rate-limit protection for availability when the
// limiter backend is down. Callers that need stricter behaviour during a
// Redis outage should make that explicit at the call site instead.
type NoopLimiter struct{}

func (NoopLimiter) Allow(_ context.Context, _ string) (bool, error) { return true, nil }
func (NoopLimiter) Reset(_ context.Context, _ string) error         { return nil }
