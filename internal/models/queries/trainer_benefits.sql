-- name: AddTrainerBenefit :one
-- Inserts one benefit at the supplied position. Used inside the
-- POST /trainers transaction so the handler computes positions 1..N
-- explicitly before sending the batch — that keeps the handler-side
-- ordering deterministic without needing a MAX(position)+1 round-trip
-- per row. The (trainer_id, position) unique index catches any handler
-- bug that would double up.
INSERT INTO trainer_benefits (trainer_id, position, title, subtext)
VALUES (
  sqlc.arg(trainer_id),
  sqlc.arg(position),
  sqlc.arg(title),
  sqlc.arg(subtext)
)
RETURNING id, trainer_id, position, title, subtext, created_at;

-- name: ListTrainerBenefits :many
SELECT id, trainer_id, position, title, subtext, created_at
FROM trainer_benefits
WHERE trainer_id = sqlc.arg(trainer_id)
ORDER BY position ASC;

-- name: DeleteTrainerBenefit :execrows
-- Deletes by benefit ID, but also requires the trainer_id to match so a
-- typo can't delete another trainer's benefit. Returns rowcount so the
-- handler can distinguish "deleted cleanly" from "wrong ID".
DELETE FROM trainer_benefits
WHERE id = sqlc.arg(id) AND trainer_id = sqlc.arg(trainer_id);

-- name: DeleteAllTrainerBenefits :exec
-- Used by the PATCH-style "replace all benefits" flow if/when we add one.
-- Kept here so the wipe-and-rewrite pattern doesn't require a manual
-- per-row delete loop.
DELETE FROM trainer_benefits WHERE trainer_id = sqlc.arg(trainer_id);
