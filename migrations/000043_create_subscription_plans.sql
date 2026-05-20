-- +goose Up
CREATE TABLE IF NOT EXISTS subscription_plans (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_type     VARCHAR(50) NOT NULL UNIQUE,
    display_name  VARCHAR(100) NOT NULL,
    sessions_total INT NOT NULL CHECK (sessions_total > 0),
    amount        BIGINT NOT NULL CHECK (amount > 0),
    currency      CHAR(3) NOT NULL,
    is_active     BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS subscription_plans;
