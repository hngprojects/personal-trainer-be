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
SET status = 'cancelled',
    cancelled_at = NOW()
WHERE id = sqlc.arg(id)
  AND status = 'active'
RETURNING *;

-- name: UpdateSubscriptionStatus :one
UPDATE subscriptions
SET status             = sqlc.arg(status),
    current_period_end = sqlc.arg(current_period_end),
    cancelled_at       = CASE
                           WHEN sqlc.arg(status) = 'cancelled' AND cancelled_at IS NULL
                           THEN NOW()
                           ELSE cancelled_at
                         END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: CountActiveSubscriptions :one
SELECT COUNT(*) FROM subscriptions
WHERE status = 'active'
  AND current_period_end > NOW();

-- name: GetRevenueSnapshot :one
-- All-time gross revenue across all statuses (active + expired + cancelled).
-- Cancelled subs are included — refund tracking is out of scope for v1.
SELECT
  CAST(COALESCE(SUM(amount), 0) AS BIGINT)                                                                          AS total_revenue,
  CAST(COALESCE(SUM(amount) FILTER (WHERE plan_type IN ('monthly_12', 'monthly_18')), 0) AS BIGINT)                AS subscription_revenue,
  CAST(COALESCE(SUM(amount) FILTER (WHERE plan_type = 'single'), 0) AS BIGINT)                                     AS one_time_revenue
FROM subscriptions
WHERE amount IS NOT NULL;

-- name: GetLatestSubscription :one
SELECT
  s.id,
  s.client_id,
  s.plan_id,
  s.plan_type,
  s.amount,
  s.currency,
  s.status,
  s.created_at,
  u.name  AS client_name,
  u.email AS client_email
FROM subscriptions s
JOIN users u ON u.id = s.client_id
WHERE s.amount IS NOT NULL
ORDER BY s.created_at DESC
LIMIT 1;
-- name: CountAdminTransactions :one
-- Backfilled from internal/repository/db/subscriptions.sql.go where it
-- was hand-added (PR #274). Lifted into the sqlc source so future
-- `sqlc generate` runs don't wipe it. Plain unfiltered count.
SELECT COUNT(*) FROM subscriptions;

-- name: ListAdminTransactions :many
-- Backfilled from PR #274 (was hand-added to the generated file).
-- Listing for the /admin/transactions dashboard — joins client +
-- trainer profiles so the table can render names without N+1 lookups.
SELECT
  s.id,
  s.client_id,
  s.trainer_id,
  s.plan_type,
  s.amount,
  s.currency,
  s.status,
  s.platform,
  s.current_period_start,
  s.current_period_end,
  s.created_at,
  s.cancelled_at,
  cu.name   AS client_name,
  cu.email  AS client_email,
  tu.name   AS trainer_name,
  tu.email  AS trainer_email
FROM subscriptions s
JOIN users cu ON cu.id = s.client_id
JOIN trainers t ON t.id = s.trainer_id
JOIN users tu ON tu.id = t.user_id
ORDER BY s.created_at DESC
LIMIT sqlc.arg('page_limit') OFFSET sqlc.arg('page_offset');

-- name: CountAdminSubscriptions :one
-- Backfilled from PR #279. Empty string in $1 means "no status filter".
SELECT COUNT(*) FROM subscriptions s WHERE ($1::text = '' OR s.status = $1);

-- name: ListAdminSubscriptions :many
-- Backfilled from PR #279. Listing for /admin/subscriptions dashboard
-- with optional status filter — empty string in $1 returns all rows.
SELECT
    s.id,
    s.client_id,
    s.trainer_id,
    s.plan_type,
    s.amount,
    s.currency,
    s.status,
    s.platform,
    s.current_period_start,
    s.current_period_end,
    s.created_at,
    s.cancelled_at,
    cu.name  AS client_name,
    cu.email AS client_email,
    tu.name  AS trainer_name,
    tu.email AS trainer_email
FROM subscriptions s
JOIN users cu ON cu.id = s.client_id
JOIN trainers t  ON t.id  = s.trainer_id
JOIN users tu ON tu.id = t.user_id
WHERE (sqlc.arg('status')::text = '' OR s.status = sqlc.arg('status'))
ORDER BY s.created_at DESC
LIMIT sqlc.arg('page_limit') OFFSET sqlc.arg('page_offset');
