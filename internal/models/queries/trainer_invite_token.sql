-- name: GetTrainerInviteTokenByToken :one
SELECT id, user_id, token, expires_at, used_at, created_at
FROM trainer_invite_tokens
WHERE token = $1
LIMIT 1;

-- name: MarkTrainerInviteTokenUsed :exec
UPDATE trainer_invite_tokens
SET used_at = NOW()
WHERE id = $1 AND used_at IS NULL;