-- +goose Up
CREATE EXTENSION IF NOT EXISTS btree_gist;

ALTER TABLE bookings ADD CONSTRAINT no_overlapping_bookings
EXCLUDE USING gist (
    trainer_id WITH =,
    tstzrange(scheduled_start, scheduled_end) WITH &&
)
WHERE (booking_status IS NULL OR booking_status NOT IN ('completed', 'cancelled', 'no_show'));

-- +goose Down 
ALTER TABLE bookings DROP CONSTRAINT IF EXISTS no_overlapping_bookings;