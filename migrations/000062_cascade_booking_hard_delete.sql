-- +goose Up
-- Fix remaining NO ACTION FK constraints that block the user hard-delete cascade.
--
-- Same payments-doesn't-exist guard as migration 000061: wrap the
-- ALTER TABLE payments blocks in DO/IF EXISTS so this migration runs
-- cleanly on environments without the payments table (which is most
-- of them — the table was authored on the feat/subscription-payment
-- branch that was never merged).

ALTER TABLE booking_session
    DROP CONSTRAINT booking_session_booking_id_fkey,
    ADD CONSTRAINT booking_session_booking_id_fkey
        FOREIGN KEY (booking_id) REFERENCES bookings(id) ON DELETE CASCADE;

ALTER TABLE bookings
    DROP CONSTRAINT bookings_subscription_id_fkey,
    ADD CONSTRAINT bookings_subscription_id_fkey
        FOREIGN KEY (subscription_id) REFERENCES subscriptions(id) ON DELETE SET NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'payments'
    ) THEN
        ALTER TABLE payments
            DROP CONSTRAINT payments_booking_id_fkey,
            ADD CONSTRAINT payments_booking_id_fkey
                FOREIGN KEY (booking_id) REFERENCES bookings(id) ON DELETE CASCADE;

        ALTER TABLE payments
            DROP CONSTRAINT payments_subscription_id_fkey,
            ADD CONSTRAINT payments_subscription_id_fkey
                FOREIGN KEY (subscription_id) REFERENCES subscriptions(id) ON DELETE SET NULL;
    END IF;
END $$;

-- +goose Down
ALTER TABLE booking_session
    DROP CONSTRAINT booking_session_booking_id_fkey,
    ADD CONSTRAINT booking_session_booking_id_fkey
        FOREIGN KEY (booking_id) REFERENCES bookings(id);

ALTER TABLE bookings
    DROP CONSTRAINT bookings_subscription_id_fkey,
    ADD CONSTRAINT bookings_subscription_id_fkey
        FOREIGN KEY (subscription_id) REFERENCES subscriptions(id);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'payments'
    ) THEN
        ALTER TABLE payments
            DROP CONSTRAINT payments_booking_id_fkey,
            ADD CONSTRAINT payments_booking_id_fkey
                FOREIGN KEY (booking_id) REFERENCES bookings(id);

        ALTER TABLE payments
            DROP CONSTRAINT payments_subscription_id_fkey,
            ADD CONSTRAINT payments_subscription_id_fkey
                FOREIGN KEY (subscription_id) REFERENCES subscriptions(id);
    END IF;
END $$;
