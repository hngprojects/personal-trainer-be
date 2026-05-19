-- name: RecordFailedVideoUpload :exec
-- Persists a terminally-failed trainer-intro-video upload so the failure is
-- queryable. The temp files are gone by the time we get here — this row is
-- for visibility/ops only, not a re-run queue.
INSERT INTO failed_video_uploads (trainer_id, object_key, attempts, last_error)
VALUES (sqlc.arg(trainer_id), sqlc.arg(object_key), sqlc.arg(attempts), sqlc.arg(last_error));
