-- name: CreateUserDevice :one
INSERT INTO user_device (user_id, device_token, platform) 
VALUES ($1, $2, $3)
ON CONFLICT (user_id, device_token)
DO UPDATE SET
    platform = EXCLUDED.platform,
    is_active = TRUE,
    is_push_notification_enabled = TRUE,
    updated_at = NOW()
RETURNING *;

-- name: GetUserDeviceByID :one
SELECT 
    id, 
    user_id,
    device_token,
    is_push_notification_enabled,
    platform,
    is_active,
    created_at,
    updated_at
FROM user_device 
WHERE id = $1
LIMIT 1;

-- name: GetDeviceByToken :one
SELECT 
    id, 
    user_id,
    device_token,
    is_push_notification_enabled,
    platform,
    is_active,
    created_at,
    updated_at
FROM user_device 
WHERE device_token = $1 
AND user_id = $2
LIMIT 1;


-- name: GetUserDevicesByUserID :many
SELECT 
    id, 
    user_id,
    device_token,
    is_push_notification_enabled,
    platform,
    is_active,
    created_at,
    updated_at
FROM user_device 
WHERE user_id = $1;

-- name: ListUserActiveDevicesByUserID :many
SELECT
    id, 
    user_id,
    device_token,
    is_push_notification_enabled,
    platform,
    is_active,
    created_at,
    updated_at
FROM user_device 
WHERE user_id = $1 
AND is_active = TRUE;

-- name: DeactivateUserDevice :exec
UPDATE user_device
SET is_active = FALSE, updated_at = NOW()
WHERE id = $1;

-- name: GetAllActiveUsersDevices :many
SELECT 
    id, 
    user_id,
    device_token,
    is_push_notification_enabled,
    platform,
    is_active,
    created_at,
    updated_at
FROM user_device
WHERE is_active = TRUE;