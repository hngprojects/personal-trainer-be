-- name: CreateTrainer :one
-- Admin-only create. Inserts the trainer row whose user_id was just provisioned
-- by UpsertTrainerUser (see users.sql). bio, intro_video_url stay NULL at
-- create time — trainers fill those in themselves once they log in with the
-- credentials emailed to them.
--
-- Field order matches the physical column order of the trainers table (see
-- migrations 005, 037, 038, 040) so sqlc reuses the db.Trainer struct rather
-- than minting per-query Row structs; the alternative breaks every helper
-- that already returns *db.Trainer.
INSERT INTO trainers (
  user_id,
  specializations,
  training_styles,
  bio,
  years_of_experience,
  display_picture,
  onboarding_status
) VALUES (
  sqlc.arg(user_id),
  sqlc.arg(specializations)::text[],
  sqlc.arg(training_styles)::text[],
  sqlc.arg(bio),
  sqlc.arg(years_of_experience),
  sqlc.arg(display_picture),
  COALESCE(sqlc.arg(onboarding_status)::text, 'pending')
)
RETURNING
  id,
  user_id,
  bio,
  years_of_experience,
  intro_video_url,
  display_picture,
  onboarding_status,
  average_rating,
  total_reviews,
  created_at,
  updated_at,
  specializations,
  training_styles;

-- name: GetTrainerByID :one
SELECT
  id,
  user_id,
  bio,
  years_of_experience,
  intro_video_url,
  display_picture,
  onboarding_status,
  average_rating,
  total_reviews,
  created_at,
  updated_at,
  specializations,
  training_styles
FROM trainers
WHERE id = $1
LIMIT 1;

-- name: ListTrainers :many
-- Filter by a single specialization. The empty-string sentinel means "no
-- filter". Containment uses the GIN index on specializations.
SELECT
  id,
  user_id,
  bio,
  years_of_experience,
  intro_video_url,
  display_picture,
  onboarding_status,
  average_rating,
  total_reviews,
  created_at,
  updated_at,
  specializations,
  training_styles
FROM trainers
WHERE ($1::text = '' OR specializations @> ARRAY[$1]::text[])
ORDER BY created_at DESC;

-- name: UpdateTrainer :one
-- Partial update. Pass NULL to leave a column unchanged. specializations and
-- training_styles use COALESCE on the array argument — pass NULL to keep
-- existing values, an empty array to clear them.
UPDATE trainers
SET
  specializations     = COALESCE(sqlc.arg(specializations)::text[], specializations),
  training_styles     = COALESCE(sqlc.arg(training_styles)::text[], training_styles),
  bio                 = COALESCE(sqlc.arg(bio), bio),
  years_of_experience = COALESCE(sqlc.arg(years_of_experience), years_of_experience),
  intro_video_url     = COALESCE(sqlc.arg(intro_video_url), intro_video_url),
  display_picture     = COALESCE(sqlc.arg(display_picture), display_picture),
  onboarding_status   = COALESCE(sqlc.narg(onboarding_status)::text, onboarding_status),
  updated_at          = NOW()
WHERE id = sqlc.arg(id)
RETURNING
  id,
  user_id,
  bio,
  years_of_experience,
  intro_video_url,
  display_picture,
  onboarding_status,
  average_rating,
  total_reviews,
  created_at,
  updated_at,
  specializations,
  training_styles;

-- name: DeleteTrainer :one
DELETE FROM trainers
WHERE id = $1
RETURNING
  id,
  user_id,
  bio,
  years_of_experience,
  intro_video_url,
  display_picture,
  onboarding_status,
  average_rating,
  total_reviews,
  created_at,
  updated_at,
  specializations,
  training_styles;

-- name: ApproveTrainer :one
UPDATE trainers
SET
  onboarding_status = 'approved',
  updated_at        = NOW()
WHERE id = $1
RETURNING
  id,
  user_id,
  bio,
  years_of_experience,
  intro_video_url,
  display_picture,
  onboarding_status,
  average_rating,
  total_reviews,
  created_at,
  updated_at,
  specializations,
  training_styles;

-- name: GetTrainerByUserID :one
SELECT
  id,
  user_id,
  bio,
  years_of_experience,
  intro_video_url,
  display_picture,
  onboarding_status,
  average_rating,
  total_reviews,
  created_at,
  updated_at,
  specializations,
  training_styles
FROM trainers
WHERE user_id = $1
LIMIT 1;

-- name: UpdateTrainerIntroVideo :execrows
-- Partial intro-video-only update written by the background video worker on
-- successful transcode + upload. Kept separate from any future
-- UpdateTrainer-everything query so the worker can't race a concurrent
-- profile edit and clobber other fields. Returns row count so the worker
-- can distinguish "updated cleanly" from "trainer was deleted between
-- upload start and DB write" — the latter is recorded as a terminal
-- failure rather than silently orphaning the object in MinIO.
UPDATE trainers
SET intro_video_url = sqlc.arg(intro_video_url),
    updated_at      = NOW()
WHERE id = sqlc.arg(id);

-- name: UpdateTrainerDisplayPicture :execrows
-- Partial display-picture-only update written by the background picture
-- worker on successful MinIO upload. Same separation rationale as
-- UpdateTrainerIntroVideo — the worker can't clobber a concurrent profile
-- edit, and the rowcount distinguishes "trainer deleted before worker
-- finished" from "updated cleanly".
UPDATE trainers
SET display_picture = sqlc.arg(display_picture),
    updated_at      = NOW()
WHERE id = sqlc.arg(id);

-- name: CountTrainers :one
SELECT COUNT(*) FROM trainers WHERE onboarding_status = 'approved';
