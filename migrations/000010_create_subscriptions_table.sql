-- +goose Up
CREATE TABLE IF NOT EXISTS subscriptions (
    id                       UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id                UUID        NOT NULL REFERENCES users(id),
    trainer_id               UUID        NOT NULL REFERENCES trainers(id),
    plan_type                VARCHAR     NOT NULL
        CHECK (plan_type IN ('one_time', 'monthly')),
    sessions_per_month       INT,
    sessions_used_this_month INT         NOT NULL DEFAULT 0,
    amount                   BIGINT,
    currency                 VARCHAR     NOT NULL DEFAULT 'NGN',
    status                   VARCHAR     NOT NULL
        CHECK (status IN ('active', 'cancelled', 'expired')),
    current_period_start     TIMESTAMPTZ,
    current_period_end       TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    cancelled_at             TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_client_id ON subscriptions(client_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_trainer_id ON subscriptions(trainer_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_status ON subscriptions(status);
CREATE INDEX IF NOT EXISTS idx_subscriptions_current_period_end ON subscriptions(current_period_end);

-- +goose Down
DROP INDEX IF EXISTS idx_subscriptions_current_period_end;
DROP INDEX IF EXISTS idx_subscriptions_status;
DROP INDEX IF EXISTS idx_subscriptions_trainer_id;
DROP INDEX IF EXISTS idx_subscriptions_client_id;
DROP TABLE IF EXISTS subscriptions;
