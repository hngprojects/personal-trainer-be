-- name: CreatePayment :one
INSERT INTO payments (
  subscription_id,
  booking_id,
  payer_id,
  provider,
  provider_transaction_id,
  idempotency_key,
  currency,
  total_amount,
  trainer_earning,
  platform_fee,
  payment_type,
  payment_status
) VALUES (
  sqlc.arg(subscription_id),
  sqlc.arg(booking_id),
  sqlc.arg(payer_id),
  sqlc.arg(provider),
  sqlc.arg(provider_transaction_id),
  sqlc.arg(idempotency_key),
  sqlc.arg(currency),
  sqlc.arg(total_amount),
  sqlc.arg(trainer_earning),
  sqlc.arg(platform_fee),
  sqlc.arg(payment_type),
  sqlc.arg(payment_status)
)
RETURNING
  id,
  subscription_id,
  booking_id,
  payer_id,
  provider,
  provider_transaction_id,
  idempotency_key,
  currency,
  total_amount,
  trainer_earning,
  platform_fee,
  payment_type,
  payment_status,
  paid_at,
  created_at;

-- name: ConfirmPayment :one
UPDATE payments
SET
  payment_status          = 'successful',
  provider_transaction_id = sqlc.arg(provider_transaction_id),
  paid_at                 = NOW()
WHERE id = sqlc.arg(id)
    AND payment_status = 'pending'
RETURNING
  id,
  subscription_id,
  booking_id,
  payer_id,
  provider,
  provider_transaction_id,
  idempotency_key,
  currency,
  total_amount,
  trainer_earning,
  platform_fee,
  payment_type,
  payment_status,
  paid_at,
  created_at;

-- name: FailPayment :one
UPDATE payments
SET payment_status = 'failed'
WHERE id = sqlc.arg(id)
    AND payment_status = 'pending'
RETURNING
  id,
  subscription_id,
  booking_id,
  payer_id,
  provider,
  provider_transaction_id,
  idempotency_key,
  currency,
  total_amount,
  trainer_earning,
  platform_fee,
  payment_type,
  payment_status,
  paid_at,
  created_at;

-- name: ListPayments :many
SELECT
  id,
  subscription_id,
  booking_id,
  payer_id,
  provider,
  provider_transaction_id,
  idempotency_key,
  currency,
  total_amount,
  trainer_earning,
  platform_fee,
  payment_type,
  payment_status,
  paid_at,
  created_at
FROM payments
WHERE payer_id = $1
ORDER BY created_at DESC
LIMIT $2
OFFSET $3;

-- name: GetPaymentByID :one
SELECT
  id,
  subscription_id,
  booking_id,
  payer_id,
  provider,
  provider_transaction_id,
  idempotency_key,
  currency,
  total_amount,
  trainer_earning,
  platform_fee,
  payment_type,
  payment_status,
  paid_at,
  created_at
FROM payments
WHERE id = $1
LIMIT 1;