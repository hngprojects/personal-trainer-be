-- +goose Up
ALTER TABLE subscriptions
    ADD COLUMN IF NOT EXISTS plan_id    VARCHAR
        CHECK (plan_id IN ('casual', 'committed', 'consistent')),
    ADD COLUMN IF NOT EXISTS platform   VARCHAR
        CHECK (platform IN ('apple', 'google')),
    ADD COLUMN IF NOT EXISTS trial_ends_at                    TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS apple_original_transaction_id    VARCHAR,
    ADD COLUMN IF NOT EXISTS google_purchase_token            TEXT;

ALTER TABLE subscriptions
    ALTER COLUMN currency SET DEFAULT 'USD';

CREATE UNIQUE INDEX IF NOT EXISTS idx_subscriptions_apple_txn
    ON subscriptions (apple_original_transaction_id)
    WHERE apple_original_transaction_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_subscriptions_google_token
    ON subscriptions (google_purchase_token)
    WHERE google_purchase_token IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_subscriptions_google_token;
DROP INDEX IF EXISTS idx_subscriptions_apple_txn;

ALTER TABLE subscriptions
    ALTER COLUMN currency SET DEFAULT 'NGN';

ALTER TABLE subscriptions
    DROP COLUMN IF EXISTS google_purchase_token,
    DROP COLUMN IF EXISTS apple_original_transaction_id,
    DROP COLUMN IF EXISTS trial_ends_at,
    DROP COLUMN IF EXISTS platform,
    DROP COLUMN IF EXISTS plan_id;
