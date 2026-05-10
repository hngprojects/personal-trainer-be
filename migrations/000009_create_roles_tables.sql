-- +goose Up
CREATE TABLE IF NOT EXISTS roles (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(50)  NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id     UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id     UUID         NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, role_id)
);

CREATE INDEX IF NOT EXISTS idx_user_roles_user_id ON user_roles(user_id);
CREATE INDEX IF NOT EXISTS idx_user_roles_role_id ON user_roles(role_id);

-- Seed the canonical roles. Names must match the AuthUserUserType enum in api.yaml.
INSERT INTO roles (name) VALUES ('client'), ('trainer'), ('admin')
ON CONFLICT (name) DO NOTHING;

-- +goose Down
DROP INDEX IF EXISTS idx_user_roles_role_id;
DROP INDEX IF EXISTS idx_user_roles_user_id;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS roles;
