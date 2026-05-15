-- +goose Up
CREATE TABLE subscriptions (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id               UUID NOT NULL REFERENCES users(id),
    trainer_id              UUID NOT NULL REFERENCES trainers(id),
    plan_type               VARCHAR NOT NULL REFERENCES subscription_plans(plan_type),
    sessions_per_month      INT NOT NULL,
    sessions_used_this_month INT NOT NULL DEFAULT 0,
    amount                  BIGINT NOT NULL,
    currency                VARCHAR NOT NULL,
    status                  VARCHAR NOT NULL,
    current_period_start    TIMESTAMPTZ NOT NULL,
    current_period_end      TIMESTAMPTZ NOT NULL,
    cancelled_at_period_end BOOLEAN NOT NULL DEFAULT FALSE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    cancelled_at            TIMESTAMPTZ
);

-- +goose Down
DROP TABLE IF EXISTS subscriptions;