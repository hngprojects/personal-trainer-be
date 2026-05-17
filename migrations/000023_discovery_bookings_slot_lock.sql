-- +goose Up
CREATE UNIQUE INDEX IF NOT EXISTS idx_discovery_bookings_slot_lock
    ON discovery_bookings (selected_datetime)
    WHERE status NOT IN ('cancelled', 'completed');

-- +goose Down
DROP INDEX IF EXISTS idx_discovery_bookings_slot_lock;
