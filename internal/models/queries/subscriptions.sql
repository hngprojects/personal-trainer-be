-- name: GetSubscriptionByID :one
SELECT * FROM subscriptions
WHERE id = sqlc.arg(id)
LIMIT 1;

-- name: GetActiveSubscriptionForClient :one
SELECT * FROM subscriptions
WHERE client_id = sqlc.arg(client_id)
  AND trainer_id = sqlc.arg(trainer_id)
  AND status = 'active'
  AND current_period_end > NOW()
LIMIT 1;

-- name: RefundSessionCredit :one
UPDATE subscriptions
SET sessions_used_this_month = GREATEST(0, sessions_used_this_month - 1)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: CreateSubscription :one
INSERT INTO subscriptions (
    client_id,
    trainer_id,
    plan_id,
    plan_type,
    platform,
    sessions_per_month,
    amount,
    currency,
    status,
    trial_ends_at,
    current_period_start,
    current_period_end,
    apple_original_transaction_id,
    google_purchase_token
) VALUES (
    sqlc.arg(client_id),
    sqlc.arg(trainer_id),
    sqlc.arg(plan_id),
    sqlc.arg(plan_type),
    sqlc.arg(platform),
    sqlc.arg(sessions_per_month),
    sqlc.arg(amount),
    'USD',
    'active',
    sqlc.arg(trial_ends_at),
    sqlc.arg(current_period_start),
    sqlc.arg(current_period_end),
    sqlc.arg(apple_original_transaction_id),
    sqlc.arg(google_purchase_token)
)
RETURNING *;

-- name: GetSubscriptionByAppleTransactionID :one
SELECT * FROM subscriptions
WHERE apple_original_transaction_id = sqlc.arg(apple_original_transaction_id)
LIMIT 1;

-- name: GetSubscriptionByGooglePurchaseToken :one
SELECT * FROM subscriptions
WHERE google_purchase_token = sqlc.arg(google_purchase_token)
LIMIT 1;

-- name: GetActiveSubscriptionByClientID :one
SELECT * FROM subscriptions
WHERE client_id = sqlc.arg(client_id)
  AND status = 'active'
  AND current_period_end > NOW()
ORDER BY created_at DESC
LIMIT 1;

-- name: CancelSubscription :one
UPDATE subscriptions
SET status = 'cancelled'
WHERE id = sqlc.arg(id)
  AND status = 'active'
RETURNING *;
