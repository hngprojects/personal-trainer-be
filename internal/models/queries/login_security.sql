-- name: UpsertLoginSecurityRow :one
INSERT INTO login_security (user_id)
VALUES ($1)
ON CONFLICT (user_id) DO UPDATE
  SET updated_at = NOW()
RETURNING user_id, failed_attempts, locked_until, last_failed_at, updated_at, created_at;

-- name: GetLoginSecurityByUserID :one
SELECT user_id, failed_attempts, locked_until, last_failed_at, updated_at, created_at
FROM login_security
WHERE user_id = $1
LIMIT 1;

-- name: IncrementFailedLoginAttempt :one
UPDATE login_security
SET failed_attempts = failed_attempts + 1,
    last_failed_at = NOW(),
    updated_at = NOW()
WHERE user_id = $1
RETURNING user_id, failed_attempts, locked_until, last_failed_at, updated_at, created_at;

-- name: LockUserLoginUntil :one
UPDATE login_security
SET locked_until = $2,
    updated_at = NOW()
WHERE user_id = $1
RETURNING user_id, failed_attempts, locked_until, last_failed_at, updated_at, created_at;

-- name: ResetLoginSecurityOnSuccess :exec
UPDATE login_security
SET failed_attempts = 0,
    locked_until = NULL,
    last_failed_at = NULL,
    updated_at = NOW()
WHERE user_id = $1;