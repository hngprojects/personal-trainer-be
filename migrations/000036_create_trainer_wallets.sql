-- +goose Up
CREATE TABLE trainer_wallets (
    trainer_id      UUID PRIMARY KEY REFERENCES trainers(id),
    current_balance BIGINT NOT NULL DEFAULT 0,
    total_earned    BIGINT NOT NULL DEFAULT 0,
    total_paid_out  BIGINT NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS trainer_wallets;