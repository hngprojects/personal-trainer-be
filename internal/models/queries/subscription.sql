-- name: GetPlanByType :one
SELECT
  id,
  plan_type,
  display_name,
  sessions_total,
  amount,
  currency,
  is_active,
  created_at,
  updated_at
FROM subscription_plans
WHERE plan_type = $1
  AND is_active = true
LIMIT 1;

-- name: GetActiveSubscription :one
SELECT
  id,
  client_id,
  trainer_id,
  plan_id,
  plan_type,
  sessions_per_month,
  sessions_used_this_month,
  amount,
  currency,
  status,
  current_period_start,
  current_period_end,
  cancelled_at_period_end,
  created_at,
  cancelled_at
FROM subscriptions
WHERE client_id = sqlc.arg(client_id)
  AND trainer_id = sqlc.arg(trainer_id)
  AND status = 'active'
LIMIT 1;

-- name: CreateSubscription :one
INSERT INTO subscriptions (
  client_id,
  trainer_id,
  plan_id,
  plan_type,
  sessions_per_month,
  sessions_used_this_month,
  amount,
  currency,
  status,
  current_period_start,
  cancelled_at_period_end,
  current_period_end
) VALUES (
  sqlc.arg(client_id),
  sqlc.arg(trainer_id),
  sqlc.arg(plan_id),
  sqlc.arg(plan_type),
  sqlc.arg(sessions_per_month),
  0,
  sqlc.arg(amount),
  sqlc.arg(currency),
  sqlc.arg(status),
  sqlc.arg(current_period_start),
  false,
  sqlc.arg(current_period_end)
)
RETURNING
  id,
  client_id,
  trainer_id,
  plan_id,
  plan_type,
  sessions_per_month,
  sessions_used_this_month,
  amount,
  currency,
  status,
  current_period_start,
  current_period_end,
  cancelled_at_period_end,
  created_at,
  cancelled_at;

-- name: ActivateSubscription :one
UPDATE subscriptions
SET status = 'active'
WHERE id = sqlc.arg(id)
RETURNING
  id,
  client_id,
  trainer_id,
  plan_id,
  plan_type,
  sessions_per_month,
  sessions_used_this_month,
  amount,
  currency,
  status,
  current_period_start,
  current_period_end,
  cancelled_at_period_end,
  created_at,
  cancelled_at;

-- name: ListSubscriptions :many
SELECT
  id,
  client_id,
  trainer_id,
  plan_id,
  plan_type,
  sessions_per_month,
  sessions_used_this_month,
  amount,
  currency,
  status,
  current_period_start,
  current_period_end,
  cancelled_at_period_end,
  created_at,
  cancelled_at
FROM subscriptions
WHERE client_id = $1
ORDER BY created_at DESC;

-- name: GetSubscriptionByID :one
SELECT
  id,
  client_id,
  trainer_id,
  plan_id,
  plan_type,
  sessions_per_month,
  sessions_used_this_month,
  amount,
  currency,
  status,
  current_period_start,
  current_period_end,
  cancelled_at_period_end,
  created_at,
  cancelled_at
FROM subscriptions
WHERE id = $1
LIMIT 1;

-- name: CountSubscriptionUsage :one
SELECT COUNT(*) 
FROM subscription_usage
WHERE subscription_id = $1;

-- name: CancelSubscription :one
UPDATE subscriptions
SET cancelled_at_period_end = true
WHERE id = sqlc.arg(id)
RETURNING
  id,
  client_id,
  trainer_id,
  plan_id,
  plan_type,
  sessions_per_month,
  sessions_used_this_month,
  amount,
  currency,
  status,
  current_period_start,
  current_period_end,
  cancelled_at_period_end,
  created_at,
  cancelled_at;
