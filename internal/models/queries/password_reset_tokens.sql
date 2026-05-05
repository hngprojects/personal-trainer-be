-- name: CreatePasswordResetToken :one
INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ClaimPasswordResetToken :one
UPDATE password_reset_tokens
SET used_at = NOW()
WHERE token_hash = $1
    AND used_at IS NULL
    AND expires_at > NOW()
RETURNING user_id;

-- name: DeletePasswordResetTokensByUserID :exec
DELETE FROM password_reset_tokens
WHERE user_id = $1;
