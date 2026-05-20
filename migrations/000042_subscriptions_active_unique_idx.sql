-- +goose Up
CREATE UNIQUE INDEX IF NOT EXISTS idx_subscriptions_active_client_trainer
  ON subscriptions (client_id, trainer_id)
  WHERE status = 'active';

-- +goose Down
DROP INDEX IF EXISTS idx_subscriptions_active_client_trainer;
