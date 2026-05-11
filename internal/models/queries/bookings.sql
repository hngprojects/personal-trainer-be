-- name: CreateBooking :one
INSERT INTO bookings (
  trainer_id,
  client_user_id,
  status,
  completed_at
) VALUES (
  sqlc.arg(trainer_id),
  sqlc.arg(client_user_id),
  COALESCE(sqlc.arg(status)::text, 'pending'),
  sqlc.arg(completed_at)
)
RETURNING
  id,
  trainer_id,
  client_user_id,
  status,
  completed_at,
  created_at,
  updated_at;

-- name: GetBookingByID :one
SELECT
  id,
  trainer_id,
  client_user_id,
  status,
  completed_at,
  created_at,
  updated_at
FROM bookings
WHERE id = $1
LIMIT 1;

-- name: GetBookingByIDForUpdate :one
SELECT
  id,
  trainer_id,
  client_user_id,
  status,
  completed_at,
  created_at,
  updated_at
FROM bookings
WHERE id = $1
LIMIT 1
FOR UPDATE;
