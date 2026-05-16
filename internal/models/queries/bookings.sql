-- name: CreateBooking :one
INSERT INTO bookings (
  trainer_id,
  client_id,
  subscription_id,
  booking_slot,
  timezone,
  booking_status,
  session_platform,
  cancellation_reason,
  created_at,
  cancelled_at
) VALUES (
  sqlc.arg(trainer_id),
  sqlc.arg(client_id),
  sqlc.arg(subscription_id),
  sqlc.arg(booking_slot),
  sqlc.arg(timezone),
  sqlc.arg(booking_status),
  sqlc.arg(session_platform),
  sqlc.arg(cancellation_reason),
  sqlc.arg(created_at),
  sqlc.arg(cancelled_at)
)
RETURNING
  id,
  trainer_id,
  client_id,
  subscription_id,
  booking_slot,
  timezone,
  booking_status,
  session_platform,
  cancellation_reason,
  created_at,
  cancelled_at;

-- name: GetBookingByID :one
SELECT
  id,
  trainer_id,
  client_id,
  subscription_id,
  booking_slot,
  timezone,
  booking_status,
  session_platform,
  cancellation_reason,
  created_at,
  cancelled_at
FROM bookings
WHERE id = $1
LIMIT 1;

-- name: GetBookingByIDForUpdate :one
SELECT
  id,
  trainer_id,
  client_id,
  subscription_id,
  booking_slot,
  timezone,
  booking_status,
  session_platform,
  cancellation_reason,
  created_at,
  cancelled_at
FROM bookings
WHERE id = $1
LIMIT 1
FOR UPDATE;

-- name: GetSubscription :one
SELECT
    id,
    client_id,
    trainer_id,
    status,
    created_at
FROM subscriptions
WHERE id = $1
AND status = 'active'
LIMIT 1;
