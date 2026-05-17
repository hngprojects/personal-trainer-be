-- +goose Up
-- failed_avatar_uploads records avatar upload jobs that exhausted all in-process
-- retries without succeeding. The original bytes are NOT retained — the user
-- must re-upload to actually get a new avatar — but the row makes the failure
-- visible (queryable by ops, surfacable via an admin dashboard later) and gives
-- us a place to track repeat offenders if MinIO or a region is misbehaving.
CREATE TABLE IF NOT EXISTS failed_avatar_uploads (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    object_key  TEXT         NOT NULL,
    -- attempts is the number of upload tries the worker made before recording
    -- this row. A 0 here would mean a logical bug in the worker pipeline, so
    -- guard against ever inserting one — better to fail loud at the DB layer
    -- than to ship misleading telemetry to ops.
    attempts    INTEGER      NOT NULL CHECK (attempts > 0),
    last_error  TEXT         NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_failed_avatar_uploads_user_id
    ON failed_avatar_uploads (user_id);

CREATE INDEX IF NOT EXISTS idx_failed_avatar_uploads_created_at
    ON failed_avatar_uploads (created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_failed_avatar_uploads_created_at;
DROP INDEX IF EXISTS idx_failed_avatar_uploads_user_id;
DROP TABLE IF EXISTS failed_avatar_uploads;
