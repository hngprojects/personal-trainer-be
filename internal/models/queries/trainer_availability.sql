-- name: DeleteTrainerAvailability :exec
DELETE FROM trainer_availability
WHERE trainer_id = $1;

-- name: CreateTrainerAvailability :one
INSERT INTO trainer_availability (trainer_id, day_of_week, start_time, end_time, timezone)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, trainer_id, day_of_week, start_time, end_time, timezone, created_at, updated_at;

-- name: ListTrainerAvailability :many
SELECT id, trainer_id, day_of_week, start_time, end_time, timezone, created_at, updated_at
FROM trainer_availability
WHERE trainer_id = $1
ORDER BY day_of_week, start_time;