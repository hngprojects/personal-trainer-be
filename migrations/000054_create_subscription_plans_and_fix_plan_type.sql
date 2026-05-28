-- +goose Up

-- Create the subscription_plans lookup table
CREATE TABLE IF NOT EXISTS subscription_plans (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_type    VARCHAR     NOT NULL UNIQUE,
    display_name VARCHAR     NOT NULL,
    sessions_total INT       NOT NULL,
    amount       BIGINT      NOT NULL,
    currency     VARCHAR     NOT NULL DEFAULT 'usd',
    is_active    BOOLEAN     NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed the three plans
INSERT INTO subscription_plans (plan_type, display_name, sessions_total, amount, currency)
VALUES
    ('single',     'Single Session',      1,  2000,  'usd'),
    ('monthly_12', 'Monthly 12 Sessions', 12, 8000,  'usd'),
    ('monthly_18', 'Monthly 18 Sessions', 18, 12000, 'usd')
ON CONFLICT (plan_type) DO NOTHING;

-- Drop the old check constraint that only allowed 'one_time' and 'monthly'
ALTER TABLE subscriptions
    DROP CONSTRAINT IF EXISTS subscriptions_plan_type_check;

-- Backfill legacy plan_type values to match the seeded subscription_plans rows
-- before adding the FK so no existing row violates the constraint.
UPDATE subscriptions SET plan_type = 'single'     WHERE plan_type = 'one_time';
UPDATE subscriptions SET plan_type = 'monthly_12' WHERE plan_type = 'monthly';
-- NULL out any remaining unrecognised values so the FK can be added safely.
UPDATE subscriptions SET plan_type = NULL
    WHERE plan_type NOT IN ('single', 'monthly_12', 'monthly_18');

-- Add FK to subscription_plans so only valid plan types can be stored
ALTER TABLE subscriptions
    DROP CONSTRAINT IF EXISTS subscriptions_plan_type_fkey;
ALTER TABLE subscriptions
    ADD CONSTRAINT subscriptions_plan_type_fkey
    FOREIGN KEY (plan_type) REFERENCES subscription_plans(plan_type);

-- +goose Down

ALTER TABLE subscriptions
    DROP CONSTRAINT IF EXISTS subscriptions_plan_type_fkey;

ALTER TABLE subscriptions
    ADD CONSTRAINT subscriptions_plan_type_check
    CHECK (plan_type IN ('one_time', 'monthly'));

DELETE FROM subscription_plans WHERE plan_type IN ('single', 'monthly_12', 'monthly_18');

DROP TABLE IF EXISTS subscription_plans;
