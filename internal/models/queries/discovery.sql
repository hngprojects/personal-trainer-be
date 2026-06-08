-- name: CreateDiscoveryBooking :one
-- messenger_handle is nullable — populated only when contact_mode =
-- 'messenger'. zoom_meeting_link / zoom_meeting_id keep their column
-- names for backward compat with existing rows but now also hold
-- google_meet URLs when contact_mode = 'google_meet'. Renaming those
-- columns to generic `meeting_link`/`meeting_id` is a planned cleanup
-- migration that we deliberately deferred to keep this PR small.
INSERT INTO discovery_bookings (
    user_id,
    name,
    email,
    contact_mode,
    phone_number,
    messenger_handle,
    selected_datetime,
    client_timezone,
    zoom_meeting_link,
    zoom_meeting_id,
    status
) VALUES (
    sqlc.arg(user_id),
    sqlc.arg(name),
    sqlc.arg(email),
    sqlc.arg(contact_mode),
    sqlc.arg(phone_number),
    sqlc.arg(messenger_handle),
    sqlc.arg(selected_datetime),
    sqlc.arg(client_timezone),
    sqlc.arg(zoom_meeting_link),
    sqlc.arg(zoom_meeting_id),
    'confirmed'
)
RETURNING *;

-- name: GetDiscoveryBookingByID :one
SELECT * FROM discovery_bookings
WHERE id = $1
LIMIT 1;

-- name: ListDiscoveryBookings :many
SELECT * FROM discovery_bookings
ORDER BY selected_datetime ASC;

-- name: ListDiscoveryBookingsPaginated :many
-- Admin paginated view of every discovery call ever booked. Newest first so
-- the most-recent activity is the top of page 1; supports
-- LIMIT/OFFSET pagination matching the admin sessions endpoint.
-- Pass empty string for status to return all statuses.
SELECT * FROM discovery_bookings
WHERE (sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text)
ORDER BY selected_datetime DESC, id DESC
LIMIT sqlc.arg(page_limit)
OFFSET sqlc.arg(page_offset);

-- name: CountDiscoveryBookings :one
-- Pass empty string for status to count all statuses.
SELECT COUNT(*) FROM discovery_bookings
WHERE (sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text);

-- name: GetActiveBookingSlots :many
SELECT * FROM booking_slots
WHERE is_active = true
ORDER BY day_of_week ASC, start_time ASC;

-- name: GetActiveBookingSlotsForDate :many
-- Same as GetActiveBookingSlots filtered to a specific date and excluding
-- discovery-booking slots already taken on that date. Discovery calls are
-- always 30 minutes, so the conflict window is fixed.
--
-- Trainer correlation: discovery_bookings.trainer_id can be NULL (slot
-- not assigned yet) or set (assigned to a specific trainer). booking_slots
-- has the same nullable shape: global slots have trainer_id IS NULL,
-- per-trainer slots have it set. A booking conflicts with a slot only
-- when their trainer_ids match — including both being NULL, which is
-- what `IS NOT DISTINCT FROM` expresses. Without this guard, one
-- trainer's discovery booking would falsely hide another trainer's
-- (or a global) discovery slot at the same time.
--
-- Timezone normalisation: bs.start_time / bs.end_time are local TIME in
-- bs.timezone — convert the booking's timestamptz to that same zone
-- before comparing, NOT to the booking's own client_timezone (those can
-- differ and lead to off-by-hours misses).
SELECT
    bs.id, bs.trainer_id, bs.day_of_week, bs.start_time, bs.end_time,
    bs.timezone, bs.is_active, bs.created_at, bs.updated_at
FROM booking_slots bs
WHERE bs.is_active = true
  AND bs.day_of_week = EXTRACT(DOW FROM sqlc.arg(target_date)::DATE)::INT
  AND NOT EXISTS (
      SELECT 1 FROM discovery_bookings db
      WHERE db.status NOT IN ('cancelled', 'completed')
        AND db.trainer_id IS NOT DISTINCT FROM bs.trainer_id
        AND (db.selected_datetime                       AT TIME ZONE COALESCE(NULLIF(bs.timezone, ''), 'UTC'))::DATE = sqlc.arg(target_date)::DATE
        AND (db.selected_datetime                       AT TIME ZONE COALESCE(NULLIF(bs.timezone, ''), 'UTC'))::TIME < bs.end_time
        AND ((db.selected_datetime + INTERVAL '30 minutes') AT TIME ZONE COALESCE(NULLIF(bs.timezone, ''), 'UTC'))::TIME > bs.start_time
  )
ORDER BY bs.day_of_week ASC, bs.start_time ASC;

-- name: GetBookingSlotByID :one
SELECT * FROM booking_slots
WHERE id = $1
LIMIT 1;

-- name: CreateBookingSlot :one
INSERT INTO booking_slots (
    day_of_week,
    start_time,
    end_time,
    timezone,
    is_active
) VALUES (
    sqlc.arg(day_of_week),
    sqlc.arg(start_time),
    sqlc.arg(end_time),
    sqlc.arg(timezone),
    true
)
RETURNING *;

-- name: UpdateBookingSlot :one
UPDATE booking_slots
SET
    day_of_week = sqlc.arg(day_of_week),
    start_time  = sqlc.arg(start_time),
    end_time    = sqlc.arg(end_time),
    timezone    = sqlc.arg(timezone),
    is_active   = sqlc.arg(is_active),
    updated_at  = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteBookingSlot :exec
DELETE FROM booking_slots
WHERE id = $1;

-- name: CheckSlotConflict :one
-- NOTE: assumes all discovery calls are 30 minutes; adjust interval if duration changes
SELECT COUNT(*) FROM discovery_bookings
WHERE selected_datetime > sqlc.arg(selected_datetime)::timestamptz - INTERVAL '30 minutes'
  AND selected_datetime < sqlc.arg(selected_datetime)::timestamptz + INTERVAL '30 minutes'
  AND status NOT IN ('cancelled', 'completed');

-- name: UpdateDiscoveryBookingZoom :one
UPDATE discovery_bookings
SET
    zoom_meeting_link = sqlc.arg(zoom_meeting_link),
    zoom_meeting_id   = sqlc.arg(zoom_meeting_id),
    updated_at        = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: GetDiscoveryBookingByUserID :one
SELECT * FROM discovery_bookings
WHERE user_id = sqlc.arg(user_id)
  AND status NOT IN ('cancelled')
LIMIT 1;

-- name: RescheduleDiscoveryBooking :one
UPDATE discovery_bookings
SET
    selected_datetime = sqlc.arg(selected_datetime)::timestamptz,
    phone_number      = sqlc.arg(phone_number),
    zoom_meeting_link = sqlc.arg(zoom_meeting_link),
    zoom_meeting_id   = sqlc.arg(zoom_meeting_id),
    reschedule_count  = reschedule_count + 1,
    updated_at        = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: CreateRescheduleHistory :exec
INSERT INTO booking_reschedule_history (
    discovery_booking_id,
    previous_datetime,
    new_datetime,
    rescheduled_by,
    reason,
    notes
) VALUES (
    sqlc.arg(discovery_booking_id),
    sqlc.arg(previous_datetime)::timestamptz,
    sqlc.arg(new_datetime)::timestamptz,
    sqlc.arg(rescheduled_by),
    sqlc.arg(reason),
    sqlc.arg(notes)
);

-- name: CheckSlotConflictExcluding :one
-- NOTE: assumes all discovery calls are 30 minutes; adjust interval if duration changes
SELECT COUNT(*) FROM discovery_bookings
WHERE selected_datetime > sqlc.arg(selected_datetime)::timestamptz - INTERVAL '30 minutes'
  AND selected_datetime < sqlc.arg(selected_datetime)::timestamptz + INTERVAL '30 minutes'
  AND status NOT IN ('cancelled', 'completed')
  AND id != sqlc.arg(exclude_id);

-- name: GetUpcomingDiscoveryBookings :many
-- Mirror of GetUpcomingPaidSessions: keep showing the call until the user
-- explicitly cancels/completes it. The 7-day grace past selected_datetime
-- bounds the visibility of abandoned bookings.
SELECT * FROM discovery_bookings
WHERE user_id = sqlc.arg(user_id)
  AND selected_datetime > NOW() - INTERVAL '7 days'
  AND status NOT IN ('cancelled', 'completed')
ORDER BY selected_datetime ASC;
