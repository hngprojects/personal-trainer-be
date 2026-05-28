-- +goose Up
-- BUG-M1-007: correct subscription plan display names and amounts to match PRD
-- PRD: Single Session $12, Standard Monthly 12 sessions $100, Premium Monthly 18 sessions $150
UPDATE subscription_plans SET
    display_name   = 'Single Session',
    amount         = 1200,
    sessions_total = 1,
    updated_at     = NOW()
WHERE plan_type = 'single';

UPDATE subscription_plans SET
    display_name   = 'Standard Monthly',
    amount         = 10000,
    sessions_total = 12,
    updated_at     = NOW()
WHERE plan_type = 'monthly_12';

UPDATE subscription_plans SET
    display_name   = 'Premium Monthly',
    amount         = 15000,
    sessions_total = 18,
    updated_at     = NOW()
WHERE plan_type = 'monthly_18';

-- +goose Down
UPDATE subscription_plans SET display_name = 'Single Session',      amount = 2000,  sessions_total = 1  WHERE plan_type = 'single';
UPDATE subscription_plans SET display_name = 'Monthly 12 Sessions', amount = 8000,  sessions_total = 12 WHERE plan_type = 'monthly_12';
UPDATE subscription_plans SET display_name = 'Monthly 18 Sessions', amount = 12000, sessions_total = 18 WHERE plan_type = 'monthly_18';
