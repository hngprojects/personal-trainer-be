-- name: CreateTrainer :one
INSERT INTO trainers (
  user_id,
  specialization,
  bio,
  years_of_experience,
  intro_video_url,
  display_picture,
  calendly_connected,
  calendly_link,
  onboarding_status
) VALUES (
  $1, $2, $3, $4, $5, $6,
  COALESCE(sqlc.arg(calendly_connected)::boolean, false),
  $8,
  COALESCE(sqlc.arg(onboarding_status)::text, 'pending')
)
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

-- name: GetTrainerByID :one
SELECT
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
  updated_at
FROM trainers
WHERE id = $1
LIMIT 1;

-- name: ListTrainers :many
SELECT
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
  updated_at
FROM trainers
WHERE ($1::text = '' OR specialization = $1)
ORDER BY created_at DESC;

-- name: UpdateTrainer :one
UPDATE trainers
SET
  specialization      = COALESCE($2, specialization),
  bio                 = COALESCE($3, bio),
  years_of_experience = COALESCE($4, years_of_experience),
  intro_video_url     = COALESCE($5, intro_video_url),
  display_picture     = COALESCE($6, display_picture),
  calendly_connected  = COALESCE($7::boolean, calendly_connected),
  calendly_link       = COALESCE($8, calendly_link),
  onboarding_status   = COALESCE($9::text, onboarding_status),
  updated_at          = NOW()
WHERE id = $1
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

-- name: DeleteTrainer :one
DELETE FROM trainers
WHERE id = $1
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
