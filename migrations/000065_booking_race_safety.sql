-- +goose Up
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- Preflight: cancel any pre-existing overlapping bookings so the
-- exclusion constraint below can actually apply. Without this, any
-- environment that already had overlaps (which is every environment
-- we've written into without race protection) fails goose with
-- SQLSTATE 23P01 and the whole deploy aborts.
--
-- Cancellation strategy is a GREEDY per-trainer keep-set: walk the
-- trainer's rows in (created_at, id) order, keep the first one, then
-- for each subsequent row cancel it iff its time range overlaps any
-- already-kept row. A naïve pairwise self-join would over-cancel in
-- overlap CHAINS — e.g. given A→B and B→C with A NOT overlapping C,
-- the greedy walk keeps A, cancels B (overlaps A), keeps C (no longer
-- overlaps any kept row). The pairwise form would incorrectly cancel
-- C too, and the Down migration deliberately can't un-cancel because
-- the cancellation_reason string isn't a reliable discriminator vs.
-- legitimate user cancels that happen to hit the same text.
--
-- Wrapped in +goose StatementBegin/End so goose sends the DO block as
-- a single statement instead of trying to split on the inner `;`.

-- +goose StatementBegin
DO $$
DECLARE
    r                       record;
    prev_trainer            uuid;
    kept_ranges             tstzrange[];
    candidate_range         tstzrange;
BEGIN
    FOR r IN
        SELECT id, trainer_id, scheduled_start, scheduled_end
        FROM bookings
        WHERE (booking_status IS NULL OR booking_status NOT IN ('completed', 'cancelled', 'no_show'))
          AND scheduled_start IS NOT NULL
          AND scheduled_end   IS NOT NULL
        ORDER BY trainer_id, created_at, id
    LOOP
        -- New trainer → reset the kept-ranges set. prev_trainer starts
        -- NULL on the first iteration, so IS DISTINCT FROM handles the
        -- initial case too.
        IF prev_trainer IS DISTINCT FROM r.trainer_id THEN
            kept_ranges  := ARRAY[]::tstzrange[];
            prev_trainer := r.trainer_id;
        END IF;

        candidate_range := tstzrange(r.scheduled_start, r.scheduled_end);

        IF EXISTS (
            SELECT 1
            FROM unnest(kept_ranges) AS existing_range
            WHERE existing_range && candidate_range
        ) THEN
            -- Overlaps an already-kept row for this trainer → cancel.
            UPDATE bookings
            SET booking_status      = 'cancelled',
                cancellation_reason = COALESCE(
                    cancellation_reason,
                    'auto-cancelled: overlapping booking detected during migration 000065'
                )
            WHERE id = r.id;
        ELSE
            -- No conflict with prior keeps → keep this row and record
            -- its range so subsequent rows in the same trainer group
            -- can be checked against it.
            kept_ranges := array_append(kept_ranges, candidate_range);
        END IF;
    END LOOP;
END $$;
-- +goose StatementEnd

ALTER TABLE bookings ADD CONSTRAINT no_overlapping_bookings
EXCLUDE USING gist (
    trainer_id WITH =,
    tstzrange(scheduled_start, scheduled_end) WITH &&
)
WHERE (booking_status IS NULL OR booking_status NOT IN ('completed', 'cancelled', 'no_show'));

-- +goose Down
ALTER TABLE bookings DROP CONSTRAINT IF EXISTS no_overlapping_bookings;
-- Deliberately NOT un-cancelling the rows the Up path cancelled.
-- Restoring them would re-introduce the overlaps that made the
-- constraint fail in the first place, and there's no way to tell
-- from the row itself which cancellations came from this migration
-- vs. legitimate user cancels that happened to hit the same
-- cancellation_reason string.
