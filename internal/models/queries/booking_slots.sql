-- name: GetBookingSlots :many
SELECT
    id,
    trainer_id,
    day_of_week,
    start_time,
    end_time,
    timezone,
    is_active,
    created_at,
    updated_at
FROM booking_slots;

-- name: GetTrainersBookingSlots :many
-- Returns bookable slots for a trainer. Joins trainers to respect the global
-- is_available toggle — when a trainer sets themselves unavailable, clients
-- see an empty schedule without the underlying slots being deleted.
SELECT
    bs.id,
    bs.day_of_week,
    bs.start_time,
    bs.end_time,
    bs.timezone,
    bs.is_active,
    bs.created_at,
    bs.updated_at
FROM booking_slots bs
JOIN trainers t ON t.id = bs.trainer_id
WHERE bs.trainer_id = $1
AND t.is_available = true;
