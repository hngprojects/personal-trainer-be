-- name: CreateDiscoveryBooking :one
INSERT INTO discovery_bookings (
    user_id,
    name,
    email,
    contact_mode,
    phone_number,
    selected_datetime,
    client_timezone,
    zoom_meeting_link,
    zoom_meeting_id,
    zoom_passcode,
    status
) VALUES (
    sqlc.arg(user_id),
    sqlc.arg(name),
    sqlc.arg(email),
    sqlc.arg(contact_mode),
    sqlc.arg(phone_number),
    sqlc.arg(selected_datetime),
    sqlc.arg(client_timezone),
    sqlc.arg(zoom_meeting_link),
    sqlc.arg(zoom_meeting_id),
    sqlc.arg(zoom_passcode),
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

-- name: GetActiveBookingSlots :many
SELECT * FROM booking_slots
WHERE is_active = true
ORDER BY day_of_week ASC, start_time ASC;

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
    zoom_passcode     = sqlc.arg(zoom_passcode),
    updated_at        = NOW()
WHERE id = sqlc.arg(id)
  AND zoom_meeting_id IS NULL
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
    zoom_passcode     = sqlc.arg(zoom_passcode),
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
SELECT * FROM discovery_bookings
WHERE user_id = sqlc.arg(user_id)
  AND selected_datetime > NOW()
  AND status NOT IN ('cancelled', 'completed')
ORDER BY selected_datetime ASC;
