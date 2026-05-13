-- +goose Up
ALTER TABLE bookings
    ADD COLUMN IF NOT EXISTS is_discovery_call BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS meeting_join_url TEXT,
    ADD COLUMN IF NOT EXISTS meeting_start_url TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_bookings_unique_discovery_per_client_trainer
    ON bookings(trainer_id, client_id)
    WHERE is_discovery_call = true;

-- +goose Down
DROP INDEX IF EXISTS idx_bookings_unique_discovery_per_client_trainer;

ALTER TABLE bookings
    DROP COLUMN IF EXISTS meeting_start_url,
    DROP COLUMN IF EXISTS meeting_join_url,
    DROP COLUMN IF EXISTS is_discovery_call;
