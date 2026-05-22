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

-- name: DeleteAvailabilitySlotByID :execrows
-- Single-row delete used by DELETE /trainers/.../availability/{slot_id}.
-- The trainer_id predicate is the data-layer authz check: even if a
-- caller guesses another trainer's slot UUID, the row won't match and
-- the handler returns 404. Returns rowsAffected so the handler can
-- distinguish "not found / not yours" from a real DB error.
DELETE FROM trainer_availability
WHERE id = sqlc.arg(id) AND trainer_id = sqlc.arg(trainer_id);

-- name: LockTrainerAvailability :exec
-- Per-trainer transaction-scoped advisory lock used by the additive POST
-- path to close the TOCTOU window between "read existing slots" and
-- "insert new slots". Released automatically at COMMIT/ROLLBACK and
-- blocks (does not error) when another TX holds it, so concurrent POSTs
-- for the same trainer queue rather than racing.
--
-- The key is hashtextextended over a salted string so different
-- per-trainer subsystems can take orthogonal locks without colliding
-- on raw UUID hashes — keep the prefix unique per use site.
SELECT pg_advisory_xact_lock(
    hashtextextended('trainer_availability:' || sqlc.arg(trainer_id)::text, 0)
);
