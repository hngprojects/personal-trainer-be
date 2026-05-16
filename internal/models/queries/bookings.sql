-- name: CreateBooking :one
INSERT INTO bookings (
  trainer_id,
  client_id,
  subscription_id,
  calendly_event_id,
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
  sqlc.arg(calendly_event_id),
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
  calendly_event_id,
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
  calendly_event_id,
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
  calendly_event_id,
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

-- name: GetUpcomingPaidSessions :many
SELECT
  b.id,
  b.trainer_id,
  b.client_id,
  b.scheduled_start,
  b.scheduled_end,
  b.timezone,
  b.booking_status,
  b.session_platform,
  b.created_at,
  u.name           AS trainer_name,
  t.specialization AS trainer_specialization,
  t.display_picture AS trainer_photo
FROM bookings b
JOIN trainers t ON t.id = b.trainer_id
JOIN users u ON u.id = t.user_id
WHERE b.client_id = sqlc.arg(client_id)
  AND b.scheduled_start > NOW()
  AND (b.booking_status IS NULL OR b.booking_status NOT IN ('cancelled', 'completed'))
ORDER BY b.scheduled_start ASC;
