-- +goose Up
CREATE TABLE trainer_wallet_ledger (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    trainer_id       UUID NOT NULL REFERENCES trainers(id),
    transaction_type VARCHAR NOT NULL, 
    reference_type   VARCHAR NOT NULL,
    reference_id     UUID NOT NULL,
    amount           BIGINT NOT NULL,
    CONSTRAINT chk_wallet_ledger_amount_positive CHECK (amount > 0),
    balance_before   BIGINT NOT NULL,
    balance_after    BIGINT NOT NULL,
    CONSTRAINT chk_balance_calculation CHECK (
        (transaction_type = 'credit' AND balance_after = balance_before + amount) OR
        (transaction_type = 'debit' AND balance_after = balance_before - amount)
    ),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Create index for faster balance history lookups
CREATE INDEX idx_ledger_trainer_created ON trainer_wallet_ledger(trainer_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS trainer_wallet_ledger;