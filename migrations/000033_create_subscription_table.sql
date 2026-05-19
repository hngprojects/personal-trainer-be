-- +goose Up
-- Add cancelled_at_period_end to existing subscriptions table (created in 000010)
ALTER TABLE subscriptions
  ADD COLUMN IF NOT EXISTS cancelled_at_period_end BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE subscriptions DROP COLUMN IF EXISTS cancelled_at_period_end;
