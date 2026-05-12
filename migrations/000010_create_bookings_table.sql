-- +goose Up
CREATE TABLE IF NOT EXISTS bookings (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    trainer_id         UUID        NOT NULL REFERENCES trainers(id),
    client_id          UUID        NOT NULL REFERENCES users(id),
    subscription_id    UUID REFERENCES subscriptions(id),
    calendly_event_id  VARCHAR,
    scheduled_start    TIMESTAMPTZ,
    scheduled_end      TIMESTAMPTZ,
    timezone           VARCHAR,
    booking_status     VARCHAR,
    session_platform   VARCHAR,
    cancellation_reason TEXT,
    created_at         TIMESTAMPTZ,
    cancelled_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_bookings_trainer_id ON bookings(trainer_id);
CREATE INDEX IF NOT EXISTS idx_bookings_client_id ON bookings(client_id);
CREATE INDEX IF NOT EXISTS idx_bookings_subscription_id ON bookings(subscription_id);
CREATE INDEX IF NOT EXISTS idx_bookings_booking_status ON bookings(booking_status);
CREATE INDEX IF NOT EXISTS idx_bookings_scheduled_start ON bookings(scheduled_start);
CREATE UNIQUE INDEX IF NOT EXISTS idx_bookings_calendly_event_id ON bookings(calendly_event_id);

-- +goose Down
DROP INDEX IF EXISTS idx_bookings_calendly_event_id;
DROP INDEX IF EXISTS idx_bookings_scheduled_start;
DROP INDEX IF EXISTS idx_bookings_booking_status;
DROP INDEX IF EXISTS idx_bookings_subscription_id;
DROP INDEX IF EXISTS idx_bookings_client_id;
DROP INDEX IF EXISTS idx_bookings_trainer_id;
DROP TABLE IF EXISTS bookings;
