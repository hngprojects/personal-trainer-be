package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter is the interface for rate limiting by key.
type RateLimiter interface {
	Allow(ctx context.Context, key string) (bool, error)
	Reset(ctx context.Context, key string) error
}

// Limiter is a Redis-backed fixed-window rate limiter.
type Limiter struct {
	client *redis.Client
	max    int
	window time.Duration
	prefix string
}

// incrScript atomically increments the counter and sets the expiry on first use.
// Using a Lua script ensures INCR and EXPIRE are executed as a single atomic operation,
// preventing keys from persisting forever if the server crashes between the two calls.
var incrScript = redis.NewScript(`
local count = redis.call("INCR", KEYS[1])
if count == 1 then
    redis.call("EXPIRE", KEYS[1], ARGV[1])
end
return count
`)

// New creates a Limiter using the provided Redis client.
// prefix namespaces keys (e.g. "rl:global"), max is the allowed requests per window.
func New(client *redis.Client, prefix string, max int, window time.Duration) *Limiter {
	return &Limiter{
		client: client,
		max:    max,
		window: window,
		prefix: prefix,
	}
}

// Allow returns true if the key is within the rate limit, false if exceeded.
// On Redis failure it fails open (allows the request) to avoid blocking legitimate users.
func (l *Limiter) Allow(ctx context.Context, key string) (bool, error) {
	redisKey := fmt.Sprintf("%s:%s", l.prefix, key)

	count, err := incrScript.Run(ctx, l.client, []string{redisKey}, int(l.window.Seconds())).Int64()
	if err != nil {
		return true, err
	}

	return count <= int64(l.max), nil
}

// Reset clears the rate limit counter for a key.
func (l *Limiter) Reset(ctx context.Context, key string) error {
	redisKey := fmt.Sprintf("%s:%s", l.prefix, key)
	return l.client.Del(ctx, redisKey).Err()
}
