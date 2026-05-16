-- +goose Up
CREATE TABLE IF NOT EXISTS bookings (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    trainer_id         UUID        NOT NULL REFERENCES trainers(id),
    client_id          UUID        NOT NULL REFERENCES users(id),
    subscription_id    UUID NOT NULL REFERENCES subscriptions(id),
    booking_slot       UUID NOT NULL REFERENCES booking_slots(id),
    timezone           VARCHAR NOT NULL,
    booking_status     VARCHAR NOT NULL DEFAULT 'pending' CHECK (booking_status IN ('pending', 'confirmed', 'cancelled', 'completed', 'no_show')),
    session_platform   VARCHAR NOT NULL DEFAULT 'zoom' CHECK (session_platform IN ('whatsapp', 'google_meet', 'zoom', 'slack')),
    cancellation_reason TEXT,
    created_at         TIMESTAMPTZ,
    cancelled_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_bookings_id ON bookings(id);


-- +goose Down
DROP INDEX IF EXISTS idx_bookings_id;
DROP TABLE IF EXISTS bookings;
