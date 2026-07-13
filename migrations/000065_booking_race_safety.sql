-- +goose Up
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- Preflight: cancel any pre-existing overlapping bookings so the
-- exclusion constraint below can actually apply. Without this, any
-- environment that already had overlaps (which is every environment
-- we've written into without race protection) fails goose with
-- SQLSTATE 23P01 and the whole deploy aborts.
--
-- Tie-breaking: keep the row with the EARLIER (created_at, id). Same
-- timestamp is resolved by UUID ordering, which is stable and total
-- so the result is deterministic even under repeat runs. Only rows
-- that are "active" (booking_status IS NULL or NOT IN cancelled/
-- completed/no_show) can conflict with the constraint's WHERE clause,
-- so we only touch those.
--
-- We stamp cancellation_reason so support can distinguish these
-- migration-driven cancels from user-requested ones. If your
-- environment doesn't have overlaps this UPDATE affects zero rows —
-- safe re-run.
UPDATE bookings b1
SET booking_status      = 'cancelled',
    cancellation_reason = COALESCE(
        b1.cancellation_reason,
        'auto-cancelled: overlapping booking detected during migration 000065'
    )
FROM bookings b2
WHERE b1.trainer_id = b2.trainer_id
  AND (b1.booking_status IS NULL OR b1.booking_status NOT IN ('completed', 'cancelled', 'no_show'))
  AND (b2.booking_status IS NULL OR b2.booking_status NOT IN ('completed', 'cancelled', 'no_show'))
  AND tstzrange(b1.scheduled_start, b1.scheduled_end) && tstzrange(b2.scheduled_start, b2.scheduled_end)
  AND (b1.created_at, b1.id) > (b2.created_at, b2.id);

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
