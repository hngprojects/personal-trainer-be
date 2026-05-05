-- name: CreatePasswordResetToken :one
INSERT INTO password_reset_tokens (user_id, token, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetPasswordResetToken :one
SELECT *
FROM password_reset_tokens
WHERE token = $1
LIMIT 1;

-- name: MarkPasswordResetTokenUsed :exec
UPDATE password_reset_tokens
SET used_at = NOW()
WHERE token = $1
    AND used_at IS NULL;

-- name: DeletePasswordResetTokensByUserID :exec
DELETE FROM password_reset_tokens
WHERE user_id = $1;
