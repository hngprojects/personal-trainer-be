-- +goose Up
ALTER TABLE discovery_bookings
    ADD COLUMN IF NOT EXISTS reschedule_count INT NOT NULL DEFAULT 0;

ALTER TABLE booking_reschedule_history
    ADD COLUMN IF NOT EXISTS notes TEXT;

-- +goose Down
ALTER TABLE booking_reschedule_history DROP COLUMN IF EXISTS notes;
ALTER TABLE discovery_bookings DROP COLUMN IF EXISTS reschedule_count;
