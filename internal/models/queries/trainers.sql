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

-- name: GetTrainerWithUserByID :one
-- Variant of GetTrainerByID that joins users so the trainer detail handler
-- can return the trainer's display name + email without a second lookup.
-- Kept distinct from GetTrainerByID so existing internal callers that
-- only need the trainers row (e.g. bookings/repository.go) continue to
-- receive db.Trainer.
SELECT
  t.id,
  t.user_id,
  t.bio,
  t.years_of_experience,
  t.intro_video_url,
  t.display_picture,
  t.onboarding_status,
  t.average_rating,
  t.total_reviews,
  t.created_at,
  t.updated_at,
  t.specializations,
  t.training_styles,
  u.name         AS trainer_name,
  u.email        AS trainer_email,
  u.gender       AS trainer_gender,
  u.phone_number AS trainer_phone_number
FROM trainers t
JOIN users u ON u.id = t.user_id
WHERE t.id = $1
LIMIT 1;

-- name: ListTrainers :many
-- Filter by a single specialization. The empty-string sentinel means "no
-- filter". Containment uses the GIN index on specializations.
--
-- Joins users so the response can render the trainer's name without an
-- extra round trip; the trainer's display name lives on users.name (the
-- trainers row only stores profile fields). Paginated via LIMIT/OFFSET —
-- callers compute total pages from CountTrainersForList.
SELECT
  t.id,
  t.user_id,
  t.bio,
  t.years_of_experience,
  t.intro_video_url,
  t.display_picture,
  t.onboarding_status,
  t.average_rating,
  t.total_reviews,
  t.created_at,
  t.updated_at,
  t.specializations,
  t.training_styles,
  u.name         AS trainer_name,
  u.email        AS trainer_email,
  u.gender       AS trainer_gender,
  u.phone_number AS trainer_phone_number
FROM trainers t
JOIN users u ON u.id = t.user_id
WHERE (sqlc.arg(category)::text = '' OR t.specializations @> ARRAY[sqlc.arg(category)::text]::text[])
ORDER BY t.created_at DESC
LIMIT sqlc.arg(page_limit)
OFFSET sqlc.arg(page_offset);

-- name: CountTrainersForList :one
-- Total count for ListTrainers, applying the same category filter. Used to
-- compute total_pages on the paginated /trainers endpoint. NOTE: this is
-- distinct from CountTrainers, which restricts to approved trainers for
-- the admin stats dashboard — here we count what the list returns.
SELECT COUNT(*) FROM trainers t
WHERE (sqlc.arg(category)::text = '' OR t.specializations @> ARRAY[sqlc.arg(category)::text]::text[]);

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


-- name: GetTrainersByBookingCountPastMonth :many
SELECT
    t.id,
    t.user_id,
    t.bio,
    t.years_of_experience,
    t.intro_video_url,
    t.display_picture,
    t.onboarding_status,
    t.average_rating,
    t.total_reviews,
    t.created_at,
    t.updated_at,
    t.specializations,
    t.training_styles,
    u.name            AS trainer_name,
    u.email           AS trainer_email,
    u.gender          AS trainer_gender,
    u.phone_number    AS trainer_phone_number,
    COUNT(b.id)       AS booking_count
FROM trainers t
JOIN users u ON u.id = t.user_id
JOIN bookings b ON b.trainer_id = t.id
WHERE b.scheduled_start >= NOW() - INTERVAL '1 month'
  AND b.scheduled_start < NOW()
  AND b.booking_status = 'completed'
GROUP BY
    t.id,
    u.name,
    u.email,
    u.gender,
    u.phone_number
ORDER BY booking_count DESC;