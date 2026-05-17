-- +goose Up
ALTER TABLE booking_slots
ADD COLUMN trainer_id UUID NOT NULL REFERENCES trainers(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_booking_slots_trainer_id ON booking_slots(trainer_id);

-- +goose Down
DROP INDEX IF EXISTS idx_booking_slots_trainer_id;
ALTER TABLE booking_slots
DROP COLUMN trainer_id;
