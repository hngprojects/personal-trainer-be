-- +goose Up
CREATE TABLE IF NOT EXISTS admin_invites (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email       CITEXT      NOT NULL,
    name        VARCHAR     NOT NULL,
    token_hash  TEXT        NOT NULL UNIQUE,
    invited_by  UUID        NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    expires_at  TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_admin_invites_email ON admin_invites(email);
CREATE INDEX IF NOT EXISTS idx_admin_invites_token_hash ON admin_invites(token_hash);

-- +goose Down
DROP TABLE IF EXISTS admin_invites;
