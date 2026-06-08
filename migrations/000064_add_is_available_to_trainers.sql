-- +goose Up

-- is_available is the trainer's global "open/closed" switch.
-- When false, clients see no bookable slots for this trainer even if
-- booking_slots rows exist. The slots are preserved — toggling back to
-- true immediately restores visibility without the trainer having to
-- re-enter their schedule.
ALTER TABLE trainers ADD COLUMN IF NOT EXISTS is_available BOOLEAN NOT NULL DEFAULT TRUE;

-- +goose Down

ALTER TABLE trainers DROP COLUMN IF EXISTS is_available;
