-- +goose Up
CREATE TABLE IF NOT EXISTS sessions (
    id         BIGSERIAL   PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token      TEXT        NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token);

-- +goose Down
DROP INDEX IF EXISTS idx_sessions_token;
DROP TABLE IF EXISTS sessions;
