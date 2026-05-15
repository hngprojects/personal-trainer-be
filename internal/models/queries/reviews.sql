-- name: CreateReview :one
INSERT INTO reviews (
  booking_id,
  trainer_id,
  client_user_id,
  rating,
  review
) VALUES (
  sqlc.arg(booking_id),
  sqlc.arg(trainer_id),
  sqlc.arg(client_user_id),
  sqlc.arg(rating),
  sqlc.arg(review)
)
RETURNING
  id,
  booking_id,
  trainer_id,
  client_user_id,
  rating,
  review,
  created_at,
  updated_at;

-- name: ListTrainerReviewsFirstPage :many
SELECT
  id,
  booking_id,
  trainer_id,
  client_user_id,
  rating,
  review,
  created_at,
  updated_at
FROM reviews
WHERE trainer_id = sqlc.arg(trainer_id)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(limit_count);

-- name: ListTrainerReviewsAfterCursor :many
SELECT
  id,
  booking_id,
  trainer_id,
  client_user_id,
  rating,
  review,
  created_at,
  updated_at
FROM reviews
WHERE trainer_id = sqlc.arg(trainer_id)
  AND (created_at, id) < (sqlc.arg(cursor_created_at)::timestamptz, sqlc.arg(cursor_id)::uuid)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(limit_count);

-- name: RefreshTrainerReviewStats :one
UPDATE trainers
SET
  average_rating = (
    SELECT AVG(rating)::numeric
    FROM reviews AS r
    WHERE r.trainer_id = sqlc.arg(trainer_id)
  ),
  total_reviews = (
    SELECT COUNT(*)::int
    FROM reviews AS r
    WHERE r.trainer_id = sqlc.arg(trainer_id)
  ),
  updated_at = NOW()
WHERE id = sqlc.arg(trainer_id)
RETURNING
  id,
  user_id,
  specialization,
  bio,
  years_of_experience,
  intro_video_url,
  display_picture,
  calendly_connected,
  calendly_link,
  onboarding_status,
  average_rating,
  total_reviews,
  created_at,
  updated_at;

-- name: GetTrainerReviews :many
SELECT *
FROM reviews
WHERE trainer_id = $1
ORDER BY created_at DESC
LIMIT $2;