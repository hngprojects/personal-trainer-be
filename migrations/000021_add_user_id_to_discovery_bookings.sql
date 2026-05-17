-- +goose Up
ALTER TABLE discovery_bookings
    ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_discovery_bookings_user_id ON discovery_bookings(user_id);

-- +goose Down
DROP INDEX IF EXISTS idx_discovery_bookings_user_id;
ALTER TABLE discovery_bookings DROP COLUMN IF EXISTS user_id;
