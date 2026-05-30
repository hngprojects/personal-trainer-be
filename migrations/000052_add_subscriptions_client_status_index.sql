-- +goose Up
CREATE INDEX IF NOT EXISTS idx_subscriptions_client_status
    ON subscriptions (client_id, status);

-- +goose Down
DROP INDEX IF EXISTS idx_subscriptions_client_status;
