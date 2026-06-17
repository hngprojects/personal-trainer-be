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

// SetFlag upserts the row and invalidates the cache. The cache invalidate
// runs AFTER the DB commit succeeds — that ordering means a concurrent
// IsEnabled either sees the OLD cached value (and re-fetches once the
// invalidate lands) or hits the DB fresh, never sees a value the DB
// doesn't yet reflect.
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
	s.invalidate(ctx, key)
	s.log.Info("feature_flags: flag updated", "key", key, "enabled", enabled, "updated_by", updatedBy)
	return rowToFlag(row), nil
}

// PublicSnapshot returns just the {key: enabled} map the mobile client
// needs on startup. Lighter than ListFlags — strips audit metadata and
// is what the no-auth GET /feature-flags returns.
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

func (s *Service) invalidate(ctx context.Context, key string) {
	if s.redis == nil {
		return
	}
	if err := s.redis.Delete(ctx, redisPrefix+key); err != nil {
		// Best-effort: a stale cache here means the change is delayed
		// by up to cacheTTL. The DB is authoritative so correctness is
		// preserved, only freshness is impacted.
		s.log.Warn("feature_flags: cache invalidate failed", "key", key, "err", err)
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
