-- +goose Up
ALTER TABLE booking_slots DROP CONSTRAINT IF EXISTS booking_slots_trainer_id_fkey;
DROP INDEX IF EXISTS idx_booking_slots_trainer_id;
ALTER TABLE booking_slots ALTER COLUMN trainer_id DROP NOT NULL;

-- +goose Down
-- Delete rows that have no trainer_id before restoring the NOT NULL constraint;
-- rows created after the Up migration ran cannot satisfy the constraint.
DELETE FROM booking_slots WHERE trainer_id IS NULL;
ALTER TABLE booking_slots ALTER COLUMN trainer_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_booking_slots_trainer_id ON booking_slots(trainer_id);
ALTER TABLE booking_slots ADD CONSTRAINT booking_slots_trainer_id_fkey
    FOREIGN KEY (trainer_id) REFERENCES trainers(id) ON DELETE CASCADE;
