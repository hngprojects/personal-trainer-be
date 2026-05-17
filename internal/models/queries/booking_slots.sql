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
SELECT
    id,
    day_of_week,
    start_time,
    end_time,
    timezone,
    is_active,
    created_at,
    updated_at
FROM booking_slots
WHERE trainer_id = $1;
