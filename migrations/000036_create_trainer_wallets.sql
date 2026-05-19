-- +goose Up
CREATE TABLE IF NOT EXISTS trainer_wallets (
    trainer_id      UUID PRIMARY KEY REFERENCES trainers(id),
    current_balance BIGINT NOT NULL DEFAULT 0,
    total_earned    BIGINT NOT NULL DEFAULT 0,
    total_paid_out  BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT chk_wallet_balances_non_negative CHECK (current_balance >= 0 AND total_earned >= 0 AND total_paid_out >= 0),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS trainer_wallets;
