-- +goose Up
CREATE TABLE IF NOT EXISTS discovery_bookings (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name                TEXT        NOT NULL,
    email               TEXT        NOT NULL,
    contact_mode        TEXT        NOT NULL CHECK (contact_mode IN ('zoom_meeting', 'phone_callback')),
    phone_number        TEXT,
    selected_datetime   TIMESTAMPTZ NOT NULL,
    client_timezone     TEXT        NOT NULL,
    zoom_meeting_link   TEXT,
    zoom_meeting_id     TEXT,
    status              TEXT        NOT NULL DEFAULT 'pending'
                            CHECK (status IN ('pending', 'confirmed', 'cancelled', 'completed')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_discovery_bookings_email ON discovery_bookings(email);
CREATE INDEX IF NOT EXISTS idx_discovery_bookings_datetime ON discovery_bookings(selected_datetime);
CREATE INDEX IF NOT EXISTS idx_discovery_bookings_status ON discovery_bookings(status);

-- +goose Down
DROP INDEX IF EXISTS idx_discovery_bookings_status;
DROP INDEX IF EXISTS idx_discovery_bookings_datetime;
DROP INDEX IF EXISTS idx_discovery_bookings_email;
DROP TABLE IF EXISTS discovery_bookings;
