-- name: DeleteSessionByToken :exec
DELETE FROM sessions WHERE token = $1;

-- name: GetUserByID :one
SELECT id, email, name, password, auth_provider, is_active, created_at, updated_at
FROM users
WHERE id = $1;

-- name: UpdateUserPassword :exec
UPDATE users
SET password = $1, updated_at = NOW()
WHERE id = $2;