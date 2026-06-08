-- name: CreateBooking :one
-- subscription_id is intentionally NOT inserted here — the field was removed
-- from the public POST /bookings contract. The column itself stays on the
-- bookings table (and on RETURNING + the other SELECT queries below) so
-- historical bookings with subscription_id populated remain queryable; new
-- bookings simply leave it NULL.
INSERT INTO bookings (
  trainer_id,
  client_id,
  scheduled_start,
  scheduled_end,
  timezone,
  booking_status,
  session_platform,
  messenger_handle,
  phone_number,
  cancellation_reason,
  created_at,
  cancelled_at
) VALUES (
  sqlc.arg(trainer_id),
  sqlc.arg(client_id),
  sqlc.arg(scheduled_start),
  sqlc.arg(scheduled_end),
  sqlc.arg(timezone),
  sqlc.arg(booking_status),
  sqlc.arg(session_platform),
  sqlc.arg(messenger_handle),
  sqlc.arg(phone_number),
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
  reschedule_count,
  messenger_handle,
  phone_number;

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
  reschedule_count,
  messenger_handle,
  phone_number
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
  reschedule_count,
  messenger_handle,
  phone_number
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
-- "Upcoming" here means any booking the client hasn't explicitly resolved
-- (cancelled or completed). We deliberately don't filter by
-- scheduled_start > NOW(): a session that came and went without being
-- started/completed must stay visible so the client/trainer can still
-- act on it. The 7-day grace past scheduled_end is an upper bound so
-- abandoned bookings eventually fall off this list.
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
  u.name             AS trainer_name,
  t.specializations  AS trainer_specializations,
  t.display_picture  AS trainer_photo
FROM bookings b
JOIN trainers t ON t.id = b.trainer_id
JOIN users u ON u.id = t.user_id
WHERE b.client_id = sqlc.arg(client_id)
  AND (b.scheduled_end IS NULL OR b.scheduled_end > NOW() - INTERVAL '7 days')
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
  reschedule_count,
  messenger_handle,
  phone_number;

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

-- name: GetTrainerUserDetails :one
SELECT
    u.id AS id,
    u.name AS name,
    u.email AS email,
    t.id AS trainer_id
FROM users u
JOIN trainers t ON u.id = t.user_id
WHERE t.id = $1
;

-- name: UpdateBookingZoom :one
UPDATE bookings
SET zoom_meeting_link = sqlc.arg(zoom_meeting_link),
    zoom_meeting_id   = sqlc.arg(zoom_meeting_id)
WHERE id = sqlc.arg(id)
  AND zoom_meeting_id IS NULL
RETURNING *;

-- name: ListBookingsForAdmin :many
-- Paginated list of every booking in the system for the admin sessions
-- dashboard. Joins client and trainer names so the response can render the
-- "client X with trainer Y" labels without forcing the FE to do N extra
-- user lookups. LEFT JOINs booking_session so the response also includes
-- the session_id (NULL if the session hasn't been started yet — the
-- booking_session row only exists after StartSession).
-- Newest first so the most recent activity surfaces on page 1.
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
  b.cancelled_at,
  b.zoom_meeting_link,
  client_user.name        AS client_name,
  client_user.email       AS client_email,
  trainer_user.name       AS trainer_name,
  trainer_user.email      AS trainer_email,
  bs.id                   AS session_id
FROM bookings b
JOIN users    client_user  ON client_user.id  = b.client_id
JOIN trainers t            ON t.id            = b.trainer_id
JOIN users    trainer_user ON trainer_user.id = t.user_id
LEFT JOIN booking_session bs ON bs.booking_id = b.id
ORDER BY b.created_at DESC NULLS LAST, b.id DESC
LIMIT sqlc.arg(page_limit)
OFFSET sqlc.arg(page_offset);

-- name: CountBookingsForAdmin :one
SELECT COUNT(*) FROM bookings;

-- name: ListActiveBookingsForAdmin :many
-- Admin view of sessions currently in progress (started or in-session). Paginated.
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
  b.cancelled_at,
  b.zoom_meeting_link,
  client_user.name        AS client_name,
  client_user.email       AS client_email,
  trainer_user.name       AS trainer_name,
  trainer_user.email      AS trainer_email,
  bs.id                   AS session_id
FROM bookings b
JOIN users    client_user  ON client_user.id  = b.client_id
JOIN trainers t            ON t.id            = b.trainer_id
JOIN users    trainer_user ON trainer_user.id = t.user_id
LEFT JOIN booking_session bs ON bs.booking_id = b.id
WHERE b.booking_status IN ('started', 'in-session')
ORDER BY b.scheduled_start ASC
LIMIT sqlc.arg(page_limit)
OFFSET sqlc.arg(page_offset);

-- name: CountActiveBookingsForAdmin :one
SELECT COUNT(*) FROM bookings WHERE booking_status IN ('started', 'in-session');

-- name: ListBookingsByTrainer :many
-- Paginated list of bookings where the caller is the trainer. The trainer
-- is identified by the trainers.id (the trainer profile row) — callers
-- resolve this from the authenticated user_id via GetTrainerByUserID.
-- Joins client name so the trainer's dashboard can render the client
-- without an extra lookup; LEFT JOINs booking_session for session_id (NULL
-- if the session hasn't been started yet).
SELECT
  b.id,
  b.trainer_id,
  b.client_id,
  b.scheduled_start,
  b.scheduled_end,
  b.timezone,
  b.booking_status,
  b.session_platform,
  b.phone_number,
  b.messenger_handle,
  b.created_at,
  b.cancelled_at,
  b.zoom_meeting_link,
  client_user.name  AS client_name,
  client_user.email AS client_email,
  bs.id             AS session_id
FROM bookings b
JOIN users client_user ON client_user.id = b.client_id
LEFT JOIN booking_session bs ON bs.booking_id = b.id
WHERE b.trainer_id = sqlc.arg(trainer_id)
ORDER BY b.scheduled_start DESC NULLS LAST, b.id DESC
LIMIT sqlc.arg(page_limit)
OFFSET sqlc.arg(page_offset);

-- name: CountBookingsByTrainer :one
SELECT COUNT(*) FROM bookings WHERE trainer_id = sqlc.arg(trainer_id);

-- name: ListTrainerClients :many
-- Distinct clients who have at least one booking with this trainer.
-- Returns the client's profile details plus aggregate booking counts and
-- the most recent booking date, so the trainer dashboard can render a
-- client roster without extra per-row lookups.
-- NOTE: all booking statuses (including cancelled) are counted. This is
-- intentional — a cancellation still records a relationship between the
-- trainer and client.
SELECT
  u.id                                                AS client_id,
  u.name                                              AS client_name,
  u.email                                             AS client_email,
  u.avatar_url                                        AS client_avatar,
  u.gender                                            AS client_gender,
  u.fitness_goals                                     AS client_fitness_goals,
  u.fitness_level                                     AS client_fitness_level,
  COUNT(b.id)::BIGINT                                 AS total_bookings,
  MAX(b.scheduled_start)::TIMESTAMPTZ                AS last_booking_date
FROM users u
JOIN bookings b ON b.client_id = u.id
WHERE b.trainer_id = sqlc.arg(trainer_id)
GROUP BY u.id
ORDER BY last_booking_date DESC NULLS LAST, u.id DESC
LIMIT sqlc.arg(page_limit)
OFFSET sqlc.arg(page_offset);

-- name: CountTrainerClients :one
-- Count of distinct clients who have booked with this trainer.
SELECT COUNT(DISTINCT client_id)::BIGINT FROM bookings WHERE trainer_id = sqlc.arg(trainer_id);

-- name: GetBookingsPastMonth :many
SELECT * FROM bookings
WHERE scheduled_start >= date_trunc('month', NOW())
  AND scheduled_start < date_trunc('month', NOW()) + INTERVAL '1 month';
-- name: AdminRescheduleBooking :one
-- Admin reschedule — no reschedule_count cap. Zoom links are cleared
-- because admin cannot provision a new Zoom meeting; clients must
-- re-join via the updated in-app join-info endpoint.
UPDATE bookings
SET scheduled_start    = sqlc.arg(scheduled_start)::timestamptz,
    scheduled_end      = sqlc.arg(scheduled_end)::timestamptz,
    zoom_meeting_link  = NULL,
    zoom_meeting_id    = NULL,
    reschedule_count   = reschedule_count + 1
WHERE id = sqlc.arg(id)
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
  reschedule_count,
  messenger_handle
  phone_number;

-- name: CheckBookingConflictForClient :one
SELECT COUNT(*) FROM bookings
WHERE trainer_id = sqlc.arg(trainer_id)
    AND scheduled_start < sqlc.arg(new_end) -- new_end
    AND scheduled_end > sqlc.arg(new_start) -- new_start
    AND (booking_status IS NULL OR booking_status NOT IN ('cancelled', 'completed', 'no_show'));
