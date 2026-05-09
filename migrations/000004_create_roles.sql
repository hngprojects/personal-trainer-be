-- +goose Up
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE IF NOT EXISTS roles (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR     NOT NULL UNIQUE,
    crated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO roles (name) VALUES ('client'), ('trainer'), ('admin'), ('super_admin')
    ON CONFLICT (name) DO NOTHING;

CREATE TABLE IF NOT EXISTS user_roles (
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id    UUID        NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, role_id)
);

CREATE INDEX IF NOT EXISTS idx_user_roles_user_id ON user_roles(user_id);

-- +goose Down
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS roles;