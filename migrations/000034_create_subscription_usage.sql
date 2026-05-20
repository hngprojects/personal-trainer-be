-- +goose Up
CREATE TABLE IF NOT EXISTS subscription_usage (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id UUID NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    booking_id      UUID NOT NULL UNIQUE REFERENCES bookings(id) ON DELETE CASCADE,
    used_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_subscription_usage_subscription_id ON subscription_usage(subscription_id);

-- +goose Down
DROP TABLE IF EXISTS subscription_usage;