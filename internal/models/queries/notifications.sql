-- name: CreateNotification :one
INSERT
INTO notification (user_id, title, message, idempotency_key) 
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetNotificationByID :one
SELECT 
    id,
    user_id,
    title,
    message,
    type,
    status,
    idempotency_key,
    retry_count,
    sent_at,
    created_at,
    updated_at
FROM notification
WHERE id=$1
LIMIT 1;

-- name: GetNotificationByUserID :many
SELECT 
    id,
    user_id,
    title,
    message,
    type,
    status,
    idempotency_key,
    retry_count,
    sent_at,
    created_at,
    updated_at
FROM notification
WHERE user_id=$1;

-- name: GetSingleUserNotification :one
SELECT 
    id,
    user_id,
    title,
    message,
    type,
    status,
    idempotency_key,
    retry_count,
    sent_at,
    created_at,
    updated_at
FROM notification
WHERE id=$1 AND user_id=$2
LIMIT 1;

-- name: GetNotificationByStatus :many
SELECT 
    id,
    user_id,
    title,
    message,
    type,
    status,
    idempotency_key,
    retry_count,
    sent_at,
    created_at,
    updated_at
FROM notification
WHERE status=$1;

-- name: UpdateNotificationStatus :exec
UPDATE notification
SET status=$1, 
    retry_count=retry_count + 1, 
    sent_at=CASE WHEN $1 = 'sent' THEN NOW() ELSE sent_at END,
    updated_at=NOW()
WHERE id=$2;

-- name: GetUserNotification :many
SELECT 
    id,
    user_id,
    title,
    message,
    type,
    status,
    idempotency_key,
    retry_count,
    sent_at,
    created_at,
    updated_at
FROM notification
WHERE user_id=$1
ORDER BY created_at DESC;

-- name: CountNotification :one
SELECT COUNT(*) FROM notification WHERE user_id=$1;