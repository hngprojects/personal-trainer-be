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
  cancelled_at,
  zoom_meeting_link,
  zoom_meeting_id,
  reschedule_count;

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
  cancelled_at,
  zoom_meeting_link,
  zoom_meeting_id,
  reschedule_count
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
  cancelled_at,
  zoom_meeting_link,
  zoom_meeting_id,
  reschedule_count
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

-- name: ReschedulePaidBooking :one
UPDATE bookings
SET scheduled_start   = sqlc.arg(scheduled_start)::timestamptz,
    scheduled_end     = sqlc.arg(scheduled_end)::timestamptz,
    zoom_meeting_link = sqlc.arg(zoom_meeting_link),
    zoom_meeting_id   = sqlc.arg(zoom_meeting_id),
    reschedule_count  = reschedule_count + 1
WHERE id = sqlc.arg(id)
  AND reschedule_count < 3
  AND (booking_status IS NULL OR booking_status NOT IN ('cancelled', 'completed'))
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
  cancelled_at,
  zoom_meeting_link,
  zoom_meeting_id,
  reschedule_count;

-- name: CheckPaidBookingConflict :one
SELECT COUNT(*) FROM bookings
WHERE trainer_id = sqlc.arg(trainer_id)
  AND id != sqlc.arg(exclude_id)
  AND scheduled_start < sqlc.arg(new_end)::timestamptz
  AND scheduled_end   > sqlc.arg(new_start)::timestamptz
  AND (booking_status IS NULL OR booking_status NOT IN ('cancelled', 'completed'));

-- name: CreatePaidRescheduleHistory :exec
INSERT INTO paid_booking_reschedule_history (booking_id, previous_start, new_start, reason)
VALUES (
  sqlc.arg(booking_id),
  sqlc.arg(previous_start)::timestamptz,
  sqlc.arg(new_start)::timestamptz,
  sqlc.arg(reason)
);
