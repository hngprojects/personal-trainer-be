-- +goose Up
ALTER TABLE users
    ALTER COLUMN email TYPE CITEXT,
    ADD COLUMN IF NOT EXISTS is_verified BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS timezone    VARCHAR NOT NULL DEFAULT 'UTC',
    ADD COLUMN IF NOT EXISTS last_login  TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS deleted_at  TIMESTAMPTZ;

ALTER TABLE users RENAME COLUMN password TO password_hash;

-- ERD removes auth_provider entirely; emails are unique globally.
ALTER TABLE users DROP COLUMN IF EXISTS auth_provider;

CREATE INDEX IF NOT EXISTS idx_users_is_active ON users(is_active);

-- +goose Down
DROP INDEX IF EXISTS idx_users_is_active;
ALTER TABLE users ADD COLUMN auth_provider TEXT NOT NULL DEFAULT 'local';
ALTER TABLE users RENAME COLUMN password_hash TO password;
ALTER TABLE users
    DROP COLUMN IF EXISTS deleted_at,
    DROP COLUMN IF EXISTS last_login,
    DROP COLUMN IF EXISTS timezone,
    DROP COLUMN IF EXISTS is_verified,
    ALTER COLUMN email TYPE TEXT;
