-- +goose Up
-- failed_video_uploads is the same shape and purpose as failed_avatar_uploads:
-- a trainer-intro-video upload job that exhausted its in-process retries
-- (transcode failure, MinIO outage, etc.) lands a row here so the failure is
-- queryable by ops. The temp files are deleted by the worker either way —
-- this row is purely a marker, not a re-run queue.
CREATE TABLE IF NOT EXISTS failed_video_uploads (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    trainer_id  UUID         NOT NULL REFERENCES trainers(id) ON DELETE CASCADE,
    object_key  TEXT         NOT NULL,
    attempts    INTEGER      NOT NULL CHECK (attempts > 0),
    last_error  TEXT         NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_failed_video_uploads_trainer_id
    ON failed_video_uploads (trainer_id);

CREATE INDEX IF NOT EXISTS idx_failed_video_uploads_created_at
    ON failed_video_uploads (created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_failed_video_uploads_created_at;
DROP INDEX IF EXISTS idx_failed_video_uploads_trainer_id;
DROP TABLE IF EXISTS failed_video_uploads;
