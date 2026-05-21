-- +goose Up
-- Holds the one-time activation token a newly-provisioned trainer (and,
-- later, any admin-invited account) uses to set their initial password.
-- Replaces the previous flow where the server generated a plaintext
-- password and emailed it; the trainer now receives a link, clicks it,
-- and supplies their own password via POST /auth/set-password.
--
-- Keyed by user_id (not email) because email is mutable and a re-invite
-- of an existing trainer must overwrite the previous token cleanly
-- without orphaning rows. ON DELETE CASCADE auto-cleans tokens when the
-- trainer's user row is deleted.
--
-- token_hash stores HMAC-SHA256(token, OTP_SECRET) — never the raw token.
-- A DB read therefore can't disclose live tokens.
--
-- consumed_at is a soft-mark on first successful use so we can detect
-- replay attempts in logs without losing the token row immediately.
CREATE TABLE IF NOT EXISTS account_setup_tokens (
    user_id     UUID        PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT        NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Lookup by token_hash is the hot path for the consume endpoint — the
-- caller supplies the token, we HMAC it, then look up the matching row.
-- UNIQUE so a (vanishingly unlikely) collision surfaces as a constraint
-- violation rather than two rows mapping to the same hash.
CREATE UNIQUE INDEX IF NOT EXISTS idx_account_setup_tokens_token_hash
    ON account_setup_tokens(token_hash);

-- Powers the periodic sweep of stale tokens.
CREATE INDEX IF NOT EXISTS idx_account_setup_tokens_expires_at
    ON account_setup_tokens(expires_at);

-- +goose Down
DROP INDEX IF EXISTS idx_account_setup_tokens_expires_at;
DROP INDEX IF EXISTS idx_account_setup_tokens_token_hash;
DROP TABLE IF EXISTS account_setup_tokens;
