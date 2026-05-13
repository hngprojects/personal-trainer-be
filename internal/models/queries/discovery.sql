-- name: CreateDiscoveryBooking :one
INSERT INTO discovery_bookings (
    name,
    email,
    contact_mode,
    phone_number,
    selected_datetime,
    client_timezone,
    zoom_meeting_link,
    zoom_meeting_id,
    status
) VALUES (
    sqlc.arg(name),
    sqlc.arg(email),
    sqlc.arg(contact_mode),
    sqlc.arg(phone_number),
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
SELECT COUNT(*) FROM discovery_bookings
WHERE selected_datetime > $1 - INTERVAL '30 minutes'
  AND selected_datetime < $1 + INTERVAL '30 minutes'
  AND status NOT IN ('cancelled');
