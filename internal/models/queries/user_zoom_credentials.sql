-- name: UpsertUserZoomCredentials :one
-- Used by the OAuth callback handler after a successful token
-- exchange. Single-row-per-user: reconnecting overwrites the previous
-- credentials in place so we never accumulate stale grants for the
-- same user. last_failure_* is cleared on every connect — a fresh
-- grant should reset the failure surface.
INSERT INTO user_zoom_credentials (
    user_id,
    access_token_enc,
    refresh_token_enc,
    access_token_expires_at,
    scope,
    zoom_user_id,
    zoom_account_id,
    zoom_email
) VALUES (
    sqlc.arg(user_id),
    sqlc.arg(access_token_enc),
    sqlc.arg(refresh_token_enc),
    sqlc.arg(access_token_expires_at),
    sqlc.arg(scope),
    sqlc.arg(zoom_user_id),
    NULLIF(sqlc.arg(zoom_account_id)::text, ''),
    NULLIF(sqlc.arg(zoom_email)::text, '')
)
ON CONFLICT (user_id) DO UPDATE
   SET access_token_enc        = EXCLUDED.access_token_enc,
       refresh_token_enc       = EXCLUDED.refresh_token_enc,
       access_token_expires_at = EXCLUDED.access_token_expires_at,
       scope                   = EXCLUDED.scope,
       zoom_user_id            = EXCLUDED.zoom_user_id,
       zoom_account_id         = EXCLUDED.zoom_account_id,
       zoom_email              = EXCLUDED.zoom_email,
       connected_at            = NOW(),
       last_failure_at         = NULL,
       last_failure_reason     = NULL
RETURNING *;

-- name: GetUserZoomCredentials :one
SELECT * FROM user_zoom_credentials WHERE user_id = $1;

-- name: UpdateUserZoomTokens :one
-- Called by the refresh path after a successful token rotation. Zoom
-- returns a NEW refresh_token on every refresh (rolling refresh
-- tokens) so both fields move together.
UPDATE user_zoom_credentials
SET access_token_enc        = sqlc.arg(access_token_enc),
    refresh_token_enc       = sqlc.arg(refresh_token_enc),
    access_token_expires_at = sqlc.arg(access_token_expires_at),
    scope                   = sqlc.arg(scope),
    last_success_at         = NOW(),
    last_failure_at         = NULL,
    last_failure_reason     = NULL
WHERE user_id = sqlc.arg(user_id)
RETURNING *;

-- name: RecordUserZoomSuccess :exec
UPDATE user_zoom_credentials
SET last_success_at     = NOW(),
    last_failure_at     = NULL,
    last_failure_reason = NULL
WHERE user_id = $1;

-- name: RecordUserZoomFailure :exec
-- Surfaces the most recent failure on the user's connection so a
-- "your Zoom connection appears broken" UI can render without us
-- having to re-attempt the call just to find out. Kept short so the
-- column doesn't grow unbounded.
UPDATE user_zoom_credentials
SET last_failure_at     = NOW(),
    last_failure_reason = LEFT(sqlc.arg(reason)::text, 500)
WHERE user_id = sqlc.arg(user_id);

-- name: DeleteUserZoomCredentials :execrows
-- DELETE /trainers/me/zoom. Returns rowcount so the handler can 404
-- if the user never connected — clearer than returning 204 either way.
DELETE FROM user_zoom_credentials WHERE user_id = $1;
