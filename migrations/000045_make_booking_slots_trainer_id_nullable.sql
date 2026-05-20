-- +goose Up
ALTER TABLE booking_slots DROP CONSTRAINT IF EXISTS booking_slots_trainer_id_fkey;
DROP INDEX IF EXISTS idx_booking_slots_trainer_id;
ALTER TABLE booking_slots ALTER COLUMN trainer_id DROP NOT NULL;

-- +goose Down
-- note: rows with NULL trainer_id will break the NOT NULL re-add if any exist
ALTER TABLE booking_slots ALTER COLUMN trainer_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_booking_slots_trainer_id ON booking_slots(trainer_id);
ALTER TABLE booking_slots ADD CONSTRAINT booking_slots_trainer_id_fkey
    FOREIGN KEY (trainer_id) REFERENCES trainers(id) ON DELETE CASCADE;
