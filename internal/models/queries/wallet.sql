-- name: UpsertTrainerWallet :one
INSERT INTO trainer_wallets (trainer_id, current_balance, total_earned, total_paid_out)
VALUES (sqlc.arg(trainer_id), sqlc.arg(amount), sqlc.arg(amount), 0)
ON CONFLICT (trainer_id) DO UPDATE
SET
  current_balance = trainer_wallets.current_balance + sqlc.arg(amount),
  total_earned    = trainer_wallets.total_earned + sqlc.arg(amount),
  updated_at      = NOW()
RETURNING
  trainer_id,
  current_balance,
  total_earned,
  total_paid_out,
  updated_at;

-- name: CreateLedgerEntry :one
INSERT INTO trainer_wallet_ledger (
  trainer_id,
  transaction_type,
  reference_type,
  reference_id,
  amount,
  balance_before,
  balance_after
) VALUES (
  sqlc.arg(trainer_id),
  sqlc.arg(transaction_type),
  sqlc.arg(reference_type),
  sqlc.arg(reference_id),
  sqlc.arg(amount),
  sqlc.arg(balance_before),
  sqlc.arg(balance_after)
)
RETURNING
  id,
  trainer_id,
  transaction_type,
  reference_type,
  reference_id,
  amount,
  balance_before,
  balance_after,
  created_at;