-- name: CreateBooking :one
INSERT INTO bookings (
  trainer_id,
  client_id,
  subscription_id,
  scheduled_start,
  scheduled_end,
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
  sqlc.arg(scheduled_start),
  sqlc.arg(scheduled_end),
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
  scheduled_start,
  scheduled_end,
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
  scheduled_start,
  scheduled_end,
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
  scheduled_start,
  scheduled_end,
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

-- name: CancelBooking :one
UPDATE bookings
SET
  booking_status = 'cancelled',
  cancellation_reason = sqlc.arg(cancellation_reason),
  cancelled_at = NOW()
WHERE id = sqlc.arg(id)
RETURNING
  id,
  trainer_id,
  client_id,
  subscription_id,
  scheduled_start,
  scheduled_end,
  timezone,
  booking_status,
  session_platform,
  cancellation_reason,
  created_at,
  cancelled_at;

-- name: ReleaseBookingSlot :execrows
-- Release a booking slot by marking it as available again
-- This updates the booking_slots table to set is_active = true for the slot used by this booking
UPDATE booking_slots
SET
  is_active = true,
  updated_at = NOW()
WHERE
  trainer_id = sqlc.arg(trainer_id)
  AND day_of_week = EXTRACT(DOW FROM sqlc.arg(scheduled_start)::TIMESTAMPTZ AT TIME ZONE sqlc.arg(timezone)::TEXT)::SMALLINT
  AND start_time = (sqlc.arg(scheduled_start)::TIMESTAMPTZ AT TIME ZONE sqlc.arg(timezone)::TEXT)::TIME
  AND end_time = (sqlc.arg(scheduled_end)::TIMESTAMPTZ AT TIME ZONE sqlc.arg(timezone)::TEXT)::TIME
  AND timezone = sqlc.arg(timezone);

-- name: GetUpcomingPaidSessions :many
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
