-- +goose Up
-- Fix remaining NO ACTION FK constraints that block the user hard-delete cascade.

ALTER TABLE booking_session
    DROP CONSTRAINT booking_session_booking_id_fkey,
    ADD CONSTRAINT booking_session_booking_id_fkey
        FOREIGN KEY (booking_id) REFERENCES bookings(id) ON DELETE CASCADE;

ALTER TABLE bookings
    DROP CONSTRAINT bookings_subscription_id_fkey,
    ADD CONSTRAINT bookings_subscription_id_fkey
        FOREIGN KEY (subscription_id) REFERENCES subscriptions(id) ON DELETE SET NULL;

ALTER TABLE payments
    DROP CONSTRAINT payments_booking_id_fkey,
    ADD CONSTRAINT payments_booking_id_fkey
        FOREIGN KEY (booking_id) REFERENCES bookings(id) ON DELETE CASCADE;

ALTER TABLE payments
    DROP CONSTRAINT payments_subscription_id_fkey,
    ADD CONSTRAINT payments_subscription_id_fkey
        FOREIGN KEY (subscription_id) REFERENCES subscriptions(id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE booking_session
    DROP CONSTRAINT booking_session_booking_id_fkey,
    ADD CONSTRAINT booking_session_booking_id_fkey
        FOREIGN KEY (booking_id) REFERENCES bookings(id);

ALTER TABLE bookings
    DROP CONSTRAINT bookings_subscription_id_fkey,
    ADD CONSTRAINT bookings_subscription_id_fkey
        FOREIGN KEY (subscription_id) REFERENCES subscriptions(id);

ALTER TABLE payments
    DROP CONSTRAINT payments_booking_id_fkey,
    ADD CONSTRAINT payments_booking_id_fkey
        FOREIGN KEY (booking_id) REFERENCES bookings(id);

ALTER TABLE payments
    DROP CONSTRAINT payments_subscription_id_fkey,
    ADD CONSTRAINT payments_subscription_id_fkey
        FOREIGN KEY (subscription_id) REFERENCES subscriptions(id);
