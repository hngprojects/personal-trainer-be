-- +goose Up
CREATE TABLE IF NOT EXISTS booking_reschedule_history (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    discovery_booking_id UUID        NOT NULL REFERENCES discovery_bookings(id) ON DELETE CASCADE,
    previous_datetime    TIMESTAMPTZ NOT NULL,
    new_datetime         TIMESTAMPTZ NOT NULL,
    rescheduled_by       TEXT        NOT NULL,
    reason               TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_reschedule_history_booking_id ON booking_reschedule_history(discovery_booking_id);

-- +goose Down
DROP INDEX IF EXISTS idx_reschedule_history_booking_id;
DROP TABLE IF EXISTS booking_reschedule_history;
