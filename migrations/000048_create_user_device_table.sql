-- +goose Up

CREATE TABLE IF NOT EXISTS user_device (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_token VARCHAR(255) NOT NULL,
    is_push_notification_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    platform VARCHAR(50) NOT NULL CHECK (platform IN ('ios', 'android', 'web')),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, device_token)
);

CREATE INDEX IF NOT EXISTS idx_user_device_user_id ON user_device(user_id);
CREATE INDEX IF NOT EXISTS idx_user_device_device_token ON user_device(device_token);

-- +goose Down
DROP TABLE IF EXISTS user_device;
DROP INDEX IF EXISTS idx_user_device_user_id;
DROP INDEX IF EXISTS idx_user_device_device_token;