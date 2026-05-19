-- +goose Up
ALTER TABLE discovery_bookings
ADD COLUMN trainer_id UUID;

CREATE INDEX IF NOT EXISTS idx_discovery_bookings_trainer_id
ON discovery_bookings(trainer_id);

-- +goose Down
DROP INDEX IF EXISTS idx_discovery_bookings_trainer_id;

ALTER TABLE discovery_bookings
DROP COLUMN IF EXISTS trainer_id;