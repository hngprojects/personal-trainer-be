-- name: GetSubscriptionByID :one
SELECT
  id,
  client_id,
  trainer_id,
  plan_type,
  sessions_per_month,
  sessions_used_this_month,
  amount,
  currency,
  status,
  current_period_start,
  current_period_end,
  created_at,
  cancelled_at,
  cancelled_at_period_end
FROM subscriptions
WHERE id = sqlc.arg(id)
LIMIT 1;

-- name: GetActiveSubscriptionForClient :one
SELECT
  id,
  client_id,
  trainer_id,
  plan_type,
  sessions_per_month,
  sessions_used_this_month,
  amount,
  currency,
  status,
  current_period_start,
  current_period_end,
  created_at,
  cancelled_at,
  cancelled_at_period_end
FROM subscriptions
WHERE client_id = sqlc.arg(client_id)
  AND trainer_id = sqlc.arg(trainer_id)
  AND status = 'active'
  AND current_period_end > NOW()
LIMIT 1;

-- name: RefundSessionCredit :one
UPDATE subscriptions
SET sessions_used_this_month = GREATEST(0, sessions_used_this_month - 1)
WHERE id = sqlc.arg(id)
RETURNING
  id,
  client_id,
  trainer_id,
  plan_type,
  sessions_per_month,
  sessions_used_this_month,
  amount,
  currency,
  status,
  current_period_start,
  current_period_end,
  created_at,
  cancelled_at,
  cancelled_at_period_end;
