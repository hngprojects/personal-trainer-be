-- +goose Up
CREATE TABLE IF NOT EXISTS password_reset_codes (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT        NOT NULL UNIQUE,
    code       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);

-- email is already indexed by the UNIQUE constraint; expires_at index helps
-- the periodic cleanup of stale rows (and the WHERE expires_at > NOW() check
-- in ConsumePasswordResetCode).
CREATE INDEX IF NOT EXISTS idx_password_reset_codes_expires_at ON password_reset_codes(expires_at);

-- +goose Down
DROP INDEX IF EXISTS idx_password_reset_codes_expires_at;
DROP TABLE IF EXISTS password_reset_codes;
