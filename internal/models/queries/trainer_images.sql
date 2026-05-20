-- name: AddTrainerImage :one
-- Inserts one image at the next free position for the trainer. The
-- COALESCE(MAX(position)+1, 1) keeps the first image at position 1 instead
-- of 0 — easier for clients (1-indexed is friendlier in URLs/UIs).
--
-- Race-safety: pg_advisory_xact_lock(hashtext(...)) serialises concurrent
-- inserts FOR THE SAME TRAINER (different trainers get different lock
-- keys, so they don't contend). Without this, two workers reading MAX
-- simultaneously would compute the same next position and the second
-- INSERT would fail the (trainer_id, position) unique index — losing a
-- gallery entry while leaving an orphaned object in MinIO. The lock is
-- released automatically when the implicit statement transaction ends.
--
-- Every use of @trainer_id is cast to ::uuid explicitly. sqlc otherwise
-- infers the parameter's Go type from the *first* cast it sees, and the
-- '::text' cast inside hashtext used to win — leaving the WHERE comparison
-- as `uuid_column = text_param`, which Postgres rejects with
-- ERRCODE 42883 "operator does not exist: uuid = text". The string concat
-- in hashtext then double-casts ::uuid::text to keep the lock key stable.
WITH lock AS (
    SELECT pg_advisory_xact_lock(hashtext('trainer_image_position:' || sqlc.arg(trainer_id)::uuid::text))
),
next_pos AS (
    SELECT COALESCE(MAX(position) + 1, 1) AS pos
    FROM trainer_images
    WHERE trainer_id = sqlc.arg(trainer_id)::uuid
)
INSERT INTO trainer_images (trainer_id, image_url, position)
SELECT sqlc.arg(trainer_id)::uuid, sqlc.arg(image_url), next_pos.pos
FROM next_pos, lock
RETURNING id, trainer_id, image_url, position, created_at;

-- name: ListTrainerImages :many
SELECT id, trainer_id, image_url, position, created_at
FROM trainer_images
WHERE trainer_id = sqlc.arg(trainer_id)
ORDER BY position ASC;

-- name: CountTrainerImages :one
SELECT COUNT(*) FROM trainer_images WHERE trainer_id = sqlc.arg(trainer_id);

-- name: DeleteTrainerImage :execrows
-- Deletes by image ID, but also requires the trainer_id match so an admin
-- can't (via a typo in the URL) delete a different trainer's image. Returns
-- row count so the handler can distinguish "deleted cleanly" from "wrong ID".
DELETE FROM trainer_images
WHERE id = sqlc.arg(id) AND trainer_id = sqlc.arg(trainer_id);
