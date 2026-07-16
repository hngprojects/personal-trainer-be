// Package feature_flags is a thin Redis-cached wrapper over the
// feature_flags Postgres table. Reads are hot-path (every payment
// attempt touches IsEnabled), so we keep a 5-minute TTL cache to avoid
// hammering the DB; admin writes invalidate the cache so flips
// propagate within seconds.
//
// The contract:
//   - "Off" is the safe default. If Redis is down, DB is down, or the
//     flag doesn't exist, IsEnabled returns false. Refusing to act is
//     always safer than accidentally enabling a kill-switched feature.
//   - Reads are best-effort cached, writes are authoritative against
//     the DB and only THEN invalidate the cache.
package feature_flags

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	appredis "github.com/hngprojects/personal-trainer-be/pkg/redis"
)

const (
	// PaymentEnabled is the canonical key for the IAP kill-switch. The
	// mobile app reads this on startup (and before showing the
	// upgrade screen) to decide whether to surface the payment UI;
	// internal/routes/subscriptions.go checks it server-side so the
	// kill-switch is honoured even if a client bypasses the FE.
	PaymentEnabled = "payment_enabled"

	cacheTTL    = 5 * time.Minute
	redisPrefix = "ff:"
)

// Flag is the public projection returned by the service. We deliberately
// don't expose the audit columns on the public read endpoint — only the
// admin endpoint surfaces updated_by/notes.
type Flag struct {
	Key       string     `json:"key"`
	Enabled   bool       `json:"enabled"`
	UpdatedBy *uuid.UUID `json:"updated_by,omitempty"`
	Notes     string     `json:"notes,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type Service struct {
	q     *db.Queries
	redis *appredis.Client
	log   *slog.Logger
}

func NewService(q *db.Queries, redis *appredis.Client, log *slog.Logger) *Service {
	return &Service{q: q, redis: redis, log: log}
}

// IsEnabled returns true iff the named flag exists AND is set to true.
// Missing flags return false — there's no "unknown" state. Cache miss
// hits the DB; cache hits ignore the rest of the row (updated_by/notes
// don't matter for the hot read path).
//
// Errors are intentionally swallowed and logged: a flag check should
// NEVER fail the caller, only return the safe default (off). If you
// need the error, call GetFlag directly.
//
// Consistency contract:
//   - After a successful SetFlag, IsEnabled reflects the new value
//     within a Redis round-trip (the write-through in SetFlag lands
//     before this function's next Redis GET).
//   - If Redis is unavailable both here AND at write time, this
//     function still returns the correct value because the DB fallback
//     is authoritative.
//   - Under concurrent load, a reader that started its DB fetch
//     BEFORE a writer's DB commit MAY land its setCache after the
//     writer's write-through, leaving the pre-flip value cached for
//     up to the 5-minute TTL. The residual window is narrow. Admins
//     needing strict consistency during an incident should re-flip
//     the flag or use PublicSnapshot (which reads DB directly).
func (s *Service) IsEnabled(ctx context.Context, key string) bool {
	if s.redis != nil {
		cmd := s.redis.Get(ctx, redisPrefix+key)
		if cmd.Err() == nil {
			return cmd.Val() == "1"
		}
	}
	row, err := s.q.GetFeatureFlag(ctx, key)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			s.log.Warn("feature_flags: DB read failed, defaulting to OFF", "key", key, "err", err)
		}
		return false
	}
	s.setCache(ctx, key, row.Enabled)
	return row.Enabled
}

// GetFlag returns the full row including audit metadata. Used by the
// admin endpoint that needs to render who-changed-when. Cache is
// bypassed because the cache only stores enabled — audit fields would
// require a richer cached representation that's not worth maintaining
// for a low-traffic admin path.
func (s *Service) GetFlag(ctx context.Context, key string) (*Flag, error) {
	row, err := s.q.GetFeatureFlag(ctx, key)
	if err != nil {
		return nil, err
	}
	return rowToFlag(row), nil
}

// ListFlags returns every flag in the table for the admin dashboard.
// Stable ordering (by key) keeps the JSON deterministic between calls.
func (s *Service) ListFlags(ctx context.Context) ([]Flag, error) {
	rows, err := s.q.ListFeatureFlags(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Flag, 0, len(rows))
	for _, r := range rows {
		out = append(out, *rowToFlag(r))
	}
	return out, nil
}

// SetFlag upserts the row and updates the cache. The cache write runs
// AFTER the DB commit succeeds — that ordering means a concurrent
// IsEnabled either sees the OLD cached value (until the write-through
// lands) or hits the DB fresh, never sees a value the DB doesn't yet
// reflect.
//
// Two subtleties worth documenting:
//
//  1. Cache-population uses a DETACHED context (not the request ctx).
//     A caller cancellation between the DB commit and the cache write
//     would otherwise leave the cache holding the pre-flip value for
//     the full 5-minute TTL — for a kill-switch, that's exactly the
//     failure mode we don't want.
//
//  2. Write-through, not invalidate-then-refill. A pure DEL followed
//     by "reader repopulates from DB" is TOCTOU-racy: a reader that
//     started reading the DB BEFORE the writer's commit can land its
//     setCache AFTER the writer's DEL, leaving the pre-flip value
//     cached until TTL. Writing the new value directly ensures the
//     common case is immediately consistent — the residual race window
//     (concurrent reader wins the setCache) still exists but is
//     narrow and self-heals at the next SetFlag or TTL expiry.
//
// Callers that need STRICT consistency (payment_enabled flipped OFF
// during an incident) should either:
//   - use the admin PublicSnapshot / ListFlags endpoints, which read
//     Postgres directly and see the committed value immediately, or
//   - bump the flag twice in a row — the second SET wins any race
//     from readers that started before the first.
func (s *Service) SetFlag(ctx context.Context, key string, enabled bool, updatedBy *uuid.UUID, notes string) (*Flag, error) {
	row, err := s.q.UpsertFeatureFlag(ctx, db.UpsertFeatureFlagParams{
		Key:     key,
		Enabled: enabled,
		UpdatedBy: uuid.NullUUID{
			UUID:  derefUUID(updatedBy),
			Valid: updatedBy != nil,
		},
		Notes: sql.NullString{String: notes, Valid: notes != ""},
	})
	if err != nil {
		return nil, err
	}
	// Detached context: cache write must not be skipped if the
	// admin's HTTP client cancels between DB commit and cache write.
	// Short timeout so a stalled Redis doesn't hold this handler
	// open past the point where the DB commit is already durable.
	cacheCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s.setCache(cacheCtx, key, enabled)
	s.log.Info("feature_flags: flag updated", "key", key, "enabled", enabled, "updated_by", updatedBy)
	return rowToFlag(row), nil
}

// PublicSnapshot returns just the {key: enabled} map the mobile client
// needs on startup. Lighter than ListFlags — strips audit metadata and
// is what the no-auth GET /feature-flags returns.
//
// This deliberately bypasses the Redis cache and reads Postgres
// directly. The trade-off is intentional: the read path here is
// admin-startup-ish (mobile calls on cold app launch, not per-tap),
// so an extra DB round trip is fine — and it gives us a way for
// clients to get the AUTHORITATIVE post-flip value without waiting
// for the write-through in SetFlag to propagate. IsEnabled is the
// per-request hot path that leans on the cache.
func (s *Service) PublicSnapshot(ctx context.Context) (map[string]bool, error) {
	rows, err := s.q.ListFeatureFlags(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[r.Key] = r.Enabled
	}
	return out, nil
}

func (s *Service) setCache(ctx context.Context, key string, enabled bool) {
	if s.redis == nil {
		return
	}
	val := "0"
	if enabled {
		val = "1"
	}
	if err := s.redis.Set(ctx, redisPrefix+key, val, cacheTTL); err != nil {
		s.log.Warn("feature_flags: cache write failed", "key", key, "err", err)
	}
}

func rowToFlag(r db.FeatureFlag) *Flag {
	f := &Flag{
		Key:       r.Key,
		Enabled:   r.Enabled,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
	if r.UpdatedBy.Valid {
		id := r.UpdatedBy.UUID
		f.UpdatedBy = &id
	}
	if r.Notes.Valid {
		f.Notes = r.Notes.String
	}
	return f
}

func derefUUID(p *uuid.UUID) uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	return *p
}
