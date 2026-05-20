-- +goose Up
CREATE TABLE IF NOT EXISTS subscription_plans (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_type     VARCHAR(50) NOT NULL UNIQUE,
    display_name  VARCHAR(100) NOT NULL,
    sessions_total INT NOT NULL CHECK (sessions_total > 0),
    amount        BIGINT NOT NULL CHECK (amount > 0),
    currency      CHAR(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    is_active     BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO subscription_plans (plan_type, display_name, sessions_total, amount, currency)
VALUES
    ('single',     'Single Session',      1,  5000, 'usd'),
    ('monthly_12', 'Monthly 12 Sessions', 12, 50000, 'usd'),
    ('monthly_18', 'Monthly 18 Sessions', 18, 65000, 'usd')
ON CONFLICT (plan_type) DO UPDATE SET
    display_name   = EXCLUDED.display_name,
    sessions_total = EXCLUDED.sessions_total,
    amount         = EXCLUDED.amount,
    currency       = EXCLUDED.currency,
    updated_at     = NOW();

-- +goose Down
DROP TABLE IF EXISTS subscription_plans;
