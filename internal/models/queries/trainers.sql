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
  sqlc.arg(user_id),
  sqlc.arg(specialization),
  sqlc.arg(bio),
  sqlc.arg(years_of_experience),
  sqlc.arg(intro_video_url),
  sqlc.arg(display_picture),
  COALESCE(sqlc.arg(calendly_connected)::boolean, false),
  sqlc.arg(calendly_link),
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
WHERE
  (
    sqlc.narg(specialization)::text IS NULL
    OR specialization = sqlc.narg(specialization)
  )
  AND (
    sqlc.narg(min_years_of_experience)::int IS NULL
    OR years_of_experience >= sqlc.narg(min_years_of_experience)
  )
  AND (
    sqlc.narg(min_average_rating) IS NULL
    OR average_rating >= sqlc.narg(min_average_rating)
  )
  AND onboarding_status = 'approved'
  
  AND (
    sqlc.narg(cursor_created_at)::timestamptz IS NULL
    OR created_at < sqlc.narg(cursor_created_at)
    OR (
      created_at = sqlc.narg(cursor_created_at)
      AND id < sqlc.narg(cursor_id)::uuid
    )
  )
ORDER BY created_at DESC, id DESC
LIMIT $1;
-- name: UpdateTrainer :one
UPDATE trainers
SET
  specialization      = COALESCE(sqlc.arg(specialization), specialization),
  bio                 = COALESCE(sqlc.arg(bio), bio),
  years_of_experience = COALESCE(sqlc.arg(years_of_experience), years_of_experience),
  intro_video_url     = COALESCE(sqlc.arg(intro_video_url), intro_video_url),
  display_picture     = COALESCE(sqlc.arg(display_picture), display_picture),
  calendly_connected  = COALESCE(sqlc.arg(calendly_connected)::boolean, calendly_connected),
  calendly_link       = COALESCE(sqlc.arg(calendly_link), calendly_link),
  onboarding_status   = COALESCE(sqlc.arg(onboarding_status)::text, onboarding_status),
  updated_at          = NOW()
WHERE id = sqlc.arg(id)
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

-- name: ApproveTrainer :one
UPDATE trainers
SET
  onboarding_status = 'approved',
  updated_at        = NOW()
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

-- name: ListPendingTrainers :many
SELECT * 
FROM trainers 
WHERE onboarding_status = 'pending' 
ORDER BY created_at DESC;

-- name: GetTrainers :many
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
ORDER BY created_at DESC; 