-- name: GetFeatureFlag :one
SELECT key, enabled, updated_by, notes, created_at, updated_at
FROM feature_flags
WHERE key = $1;

-- name: ListFeatureFlags :many
-- Ordered by key for stable response shape — both the public read and
-- the admin list endpoint depend on this for deterministic JSON output.
SELECT key, enabled, updated_by, notes, created_at, updated_at
FROM feature_flags
ORDER BY key ASC;

-- name: UpsertFeatureFlag :one
-- Single-statement write: insert if missing, update if present. Admin
-- UI calls this when an operator flips a flag — having one entry point
-- avoids a TOCTOU race between "does this flag exist yet" and "now
-- update it".
INSERT INTO feature_flags (key, enabled, updated_by, notes)
VALUES (sqlc.arg(key), sqlc.arg(enabled), sqlc.arg(updated_by), sqlc.arg(notes))
ON CONFLICT (key) DO UPDATE
SET enabled    = EXCLUDED.enabled,
    updated_by = EXCLUDED.updated_by,
    notes      = EXCLUDED.notes,
    updated_at = NOW()
RETURNING key, enabled, updated_by, notes, created_at, updated_at;
