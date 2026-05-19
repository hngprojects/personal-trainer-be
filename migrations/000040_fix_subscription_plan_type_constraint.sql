-- +goose Up
ALTER TABLE subscriptions
  DROP CONSTRAINT IF EXISTS subscriptions_plan_type_check;

ALTER TABLE subscriptions
  ADD CONSTRAINT subscriptions_plan_type_check
    CHECK (plan_type IN ('single', 'monthly_12', 'monthly_18'));

-- +goose Down
ALTER TABLE subscriptions
  DROP CONSTRAINT IF EXISTS subscriptions_plan_type_check;

-- Remap new plan types back to legacy values before reinstating the old constraint
UPDATE subscriptions SET plan_type = 'one_time' WHERE plan_type = 'single';
UPDATE subscriptions SET plan_type = 'monthly'  WHERE plan_type IN ('monthly_12', 'monthly_18');

ALTER TABLE subscriptions
  ADD CONSTRAINT subscriptions_plan_type_check
    CHECK (plan_type IN ('one_time', 'monthly'));
