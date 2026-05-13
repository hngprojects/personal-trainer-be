package bookings

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	appredis "github.com/hngprojects/personal-trainer-be/pkg/redis"
)

const (
	defaultStateTTL = 10 * time.Minute
)

type StateTracker interface {
	Track(ctx context.Context, slotID, clientID uuid.UUID, state string)
}

type noopStateTracker struct{}

func (n noopStateTracker) Track(context.Context, uuid.UUID, uuid.UUID, string) {}

type redisStateTracker struct {
	redis appredis.RedisClient
	log   *slog.Logger
	ttl   time.Duration
}

func NewRedisStateTracker(redis appredis.RedisClient, log *slog.Logger, ttl time.Duration) StateTracker {
	if ttl <= 0 {
		ttl = defaultStateTTL
	}
	if redis == nil {
		return noopStateTracker{}
	}
	return &redisStateTracker{
		redis: redis,
		log:   log,
		ttl:   ttl,
	}
}

func (r *redisStateTracker) Track(ctx context.Context, slotID, clientID uuid.UUID, state string) {
	stateKey := fmt.Sprintf("booking:slot:%s:state", slotID.String())
	if err := r.redis.Set(ctx, stateKey, state, r.ttl); err != nil {
		r.log.Warn("failed to track booking slot state in redis", "slot_id", slotID.String(), "state", state, "err", err)
	}

	ownerKey := fmt.Sprintf("booking:slot:%s:owner", slotID.String())
	if err := r.redis.Set(ctx, ownerKey, clientID.String(), r.ttl); err != nil {
		r.log.Warn("failed to track booking slot owner in redis", "slot_id", slotID.String(), "client_id", clientID.String(), "err", err)
	}
}
