-- +goose Up
CREATE TABLE subscription_plans (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_type     VARCHAR NOT NULL UNIQUE,
    display_name  VARCHAR NOT NULL,
    sessions_total INT NOT NULL,
    amount        BIGINT NOT NULL,
    currency      VARCHAR NOT NULL,
    is_active     BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS subscription_plans;