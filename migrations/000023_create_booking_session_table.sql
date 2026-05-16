-- +goose Up
CREATE TABLE IF NOT EXISTS booking_session (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id UUID NOT NULL REFERENCES bookings(id) UNIQUE,
    actual_start TIMESTAMPTZ,
    actual_end TIMESTAMPTZ,
    trainer_joined BOOLEAN DEFAULT FALSE,
    client_joined BOOLEAN DEFAULT FALSE,
    status VARCHAR(40) NOT NULL DEFAULT 'booked' CHECK (status IN ('booked', 'started', 'in-session', 'completed')),
    trainer_notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_booking_session_id ON booking_session(id);

-- +goose Down
DROP INDEX IF EXISTS idx_booking_session_id;
DROP TABLE IF EXISTS booking_session;
