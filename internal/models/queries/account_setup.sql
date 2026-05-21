-- name: UpsertAccountSetupToken :exec
-- Issue (or rotate) the setup token for a user. Single-statement
-- INSERT … ON CONFLICT (user_id) so a re-invite atomically clears the
-- previous token and re-arms consumed_at to NULL. The PRIMARY KEY on
-- user_id makes this race-free without an outer transaction.
INSERT INTO account_setup_tokens (user_id, token_hash, expires_at)
VALUES (sqlc.arg(user_id), sqlc.arg(token_hash), sqlc.arg(expires_at))
ON CONFLICT (user_id) DO UPDATE
SET token_hash  = EXCLUDED.token_hash,
    expires_at  = EXCLUDED.expires_at,
    consumed_at = NULL,
    created_at  = NOW();

-- name: ConsumeAccountSetupToken :one
-- Atomic consume: marks the row as used (consumed_at = NOW()) and returns
-- the matching user_id only if the token is unconsumed AND unexpired.
-- The single UPDATE … RETURNING form leaves no read-modify-write window
-- where two concurrent set-password requests could both succeed.
-- A second use of the same token returns 0 rows because consumed_at is
-- no longer NULL.
UPDATE account_setup_tokens
SET consumed_at = NOW()
WHERE token_hash = sqlc.arg(token_hash)
  AND consumed_at IS NULL
  AND expires_at > NOW()
RETURNING user_id;

-- name: GetAccountSetupTokenStatus :one
-- Look up by user_id (NOT by token) so the admin "is this trainer
-- already activated?" check doesn't need the original token. Returns the
-- consumed_at column so callers can distinguish "never invited",
-- "invite pending", and "already activated". Used by the trainer create
-- handler when an admin re-invites an existing email.
SELECT consumed_at, expires_at
FROM account_setup_tokens
WHERE user_id = $1;

-- name: DeleteExpiredAccountSetupTokens :exec
-- Periodic sweep of stale rows. Safe to run on a cron — leaves
-- still-valid and recently-consumed tokens untouched. consumed_at rows
-- are kept until they also expire so replays of a freshly-used token
-- still get a clean "invalid or expired" rather than slipping through
-- as a not-found.
DELETE FROM account_setup_tokens
WHERE expires_at < NOW();

-- name: UpdateUserPasswordByID :one
-- ID-keyed variant of UpdateUserPassword used by the account-setup flow.
-- That flow only knows the user_id (resolved from the supplied token);
-- the email-keyed UpdateUserPassword would require an extra lookup and
-- leak email-vs-not-email through wall-clock time. Same is_active +
-- auth_provider guard so a deactivated or social-only account can't be
-- silently password-set.
UPDATE users SET password = $2, updated_at = NOW()
WHERE id = $1 AND auth_provider = 'local' AND is_active = true
RETURNING *;
