-- name: CreateSession :one
INSERT INTO sessions (user_id, token, expires_at) VALUES ($1, $2, $3) RETURNING *;

-- name: GetSessionByToken :one
SELECT * FROM sessions WHERE token = $1 AND expires_at > NOW() LIMIT 1;

-- name: GetActiveSessionByUserID :one
SELECT * FROM sessions WHERE user_id = $1 AND expires_at > NOW() ORDER BY expires_at DESC LIMIT 1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token = $1;
