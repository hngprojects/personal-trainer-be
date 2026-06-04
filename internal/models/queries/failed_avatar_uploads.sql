-- name: RecordFailedAvatarUpload :exec
-- Persists a terminally-failed avatar upload so the failure is queryable.
-- The bytes themselves are gone — this row is for visibility/ops only.
INSERT INTO failed_avatar_uploads (user_id, object_key, attempts, last_error)
VALUES (sqlc.arg(user_id), sqlc.arg(object_key), sqlc.arg(attempts), sqlc.arg(last_error));
