-- name: CreateBookingSlot :one
INSERT INTO booking_slots (
  trainer_id,
  starts_at,
  ends_at,
  timezone,
  status,
  locked_by,
  locked_at
) VALUES (
  sqlc.arg(trainer_id),
  sqlc.arg(starts_at),
  sqlc.arg(ends_at),
  COALESCE(sqlc.arg(timezone), 'UTC'),
  COALESCE(sqlc.arg(status), 'available'),
  sqlc.arg(locked_by),
  sqlc.arg(locked_at)
)
RETURNING
  id,
  trainer_id,
  starts_at,
  ends_at,
  timezone,
  status,
  locked_by,
  locked_at,
  booking_id,
  created_at,
  updated_at;

-- name: GetBookingSlotByID :one
SELECT
  id,
  trainer_id,
  starts_at,
  ends_at,
  timezone,
  status,
  locked_by,
  locked_at,
  booking_id,
  created_at,
  updated_at
FROM booking_slots
WHERE id = $1
LIMIT 1;

-- name: GetBookingSlotByIDForUpdate :one
SELECT
  id,
  trainer_id,
  starts_at,
  ends_at,
  timezone,
  status,
  locked_by,
  locked_at,
  booking_id,
  created_at,
  updated_at
FROM booking_slots
WHERE id = $1
LIMIT 1
FOR UPDATE;

-- name: LockBookingSlotIfAvailable :one
UPDATE booking_slots
SET
  status = 'locked',
  locked_by = sqlc.arg(locked_by),
  locked_at = NOW(),
  updated_at = NOW()
WHERE id = sqlc.arg(slot_id)
  AND trainer_id = sqlc.arg(trainer_id)
  AND status = 'available'
RETURNING
  id,
  trainer_id,
  starts_at,
  ends_at,
  timezone,
  status,
  locked_by,
  locked_at,
  booking_id,
  created_at,
  updated_at;

-- name: MarkBookingSlotBooked :one
UPDATE booking_slots
SET
  status = 'booked',
  booking_id = sqlc.arg(booking_id),
  locked_by = NULL,
  locked_at = NULL,
  updated_at = NOW()
WHERE id = sqlc.arg(slot_id)
RETURNING
  id,
  trainer_id,
  starts_at,
  ends_at,
  timezone,
  status,
  locked_by,
  locked_at,
  booking_id,
  created_at,
  updated_at;
