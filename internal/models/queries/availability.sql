-- name: DeleteTrainerAvailabilitySlots :exec
DELETE FROM trainer_availability
WHERE trainer_id = $1;

-- name: InsertAvailabilitySlot :one
INSERT INTO trainer_availability (
  trainer_id,
  day_of_week,
  start_time,
  end_time,
  timezone
) VALUES (
  sqlc.arg(trainer_id),
  sqlc.arg(day_of_week),
  sqlc.arg(start_time),
  sqlc.arg(end_time),
  sqlc.arg(timezone)
)
RETURNING
  id,
  trainer_id,
  day_of_week,
  start_time,
  end_time,
  timezone,
  created_at,
  updated_at;

-- name: GetTrainerAvailabilitySlots :many
SELECT
  id,
  trainer_id,
  day_of_week,
  start_time,
  end_time,
  timezone,
  created_at,
  updated_at
FROM trainer_availability
WHERE trainer_id = $1
ORDER BY day_of_week ASC, start_time ASC;
