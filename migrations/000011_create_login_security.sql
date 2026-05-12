-- +goose Up
CREATE TABLE IF NOT EXISTS login_security (
  user_id                UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  failed_attempts        INT NOT NULL DEFAULT 0,
  locked_until           TIMESTAMPTZ NULL,
  last_failed_at         TIMESTAMPTZ NULL,
  updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_login_security_locked_until ON login_security(locked_until);

-- +goose Down
DROP TABLE IF EXISTS login_security;
