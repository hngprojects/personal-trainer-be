-- +goose Up
CREATE TABLE IF NOT EXISTS notification (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    message TEXT NOT NULL,
    type VARCHAR(50) NOT NULL CHECK (type IN ('sms', 'email', 'push')) DEFAULT 'push',
    status VARCHAR(50) NOT NULL CHECK (status IN ('pending', 'sent', 'failed', 'skipped')) DEFAULT 'pending',
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    retry_count INT NOT NULL DEFAULT 0,
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_notification_user_id ON NOTIFICATION(user_id);
CREATE INDEX IF NOT EXISTS idx_notification_status ON NOTIFICATION(status); 


-- +goose Down
DROP TABLE IF EXISTS notification;
DROP INDEX IF EXISTS idx_notification_user_id;
DROP INDEX IF EXISTS idx_notification_status;