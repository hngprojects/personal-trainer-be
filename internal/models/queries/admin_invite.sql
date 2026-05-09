-- name: CreateAdminInvite :one
INSERT INTO admin_invites (email, name, token_hash, invited_by, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetAdminInviteByTokenHash :one
SELECT * FROM admin_invites WHERE token_hash = $1 LIMIT 1;

-- name: MarkAdminInviteAccepted :exec
UPDATE admin_invites SET accepted_at = NOW() WHERE id = $1;

-- name: HasPendingInviteForEmail :one
SELECT EXISTS (
    SELECT 1 FROM admin_invites
    WHERE email = $1
      AND accepted_at IS NULL
      AND revoked_at IS NULL
      AND expires_at > NOW()
) AS has_pending;
