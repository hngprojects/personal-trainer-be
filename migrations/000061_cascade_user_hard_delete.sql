-- +goose Up
-- Add ON DELETE CASCADE to FK constraints that block hard-deleting a user.
-- bookings, subscriptions, and payments were missing CASCADE — the others
-- already had it from their original migrations.
--
-- NOTE on `payments`: the table was authored on a feature branch
-- (feat/subscription-payment) that was never merged to dev/staging/main.
-- This migration originally assumed payments existed and broke the deploy
-- when it didn't. Wrapping that block in a DO/IF EXISTS lets the deploy
-- proceed on databases without payments; once the payments feature lands,
-- the same block will run as intended on environments that grow the
-- table later. The same conditional shape covers the Down for symmetry.

ALTER TABLE bookings
    DROP CONSTRAINT bookings_client_id_fkey,
    ADD CONSTRAINT bookings_client_id_fkey
        FOREIGN KEY (client_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE subscriptions
    DROP CONSTRAINT subscriptions_client_id_fkey,
    ADD CONSTRAINT subscriptions_client_id_fkey
        FOREIGN KEY (client_id) REFERENCES users(id) ON DELETE CASCADE;

-- Goose splits SQL on `;` by default, which breaks DO blocks because
-- the inner ALTERs end with `;`. StatementBegin/End tells goose to
-- send everything between the markers as a single statement.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'payments'
    ) THEN
        ALTER TABLE payments
            DROP CONSTRAINT payments_payer_id_fkey,
            ADD CONSTRAINT payments_payer_id_fkey
                FOREIGN KEY (payer_id) REFERENCES users(id) ON DELETE CASCADE;
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE bookings
    DROP CONSTRAINT bookings_client_id_fkey,
    ADD CONSTRAINT bookings_client_id_fkey
        FOREIGN KEY (client_id) REFERENCES users(id);

ALTER TABLE subscriptions
    DROP CONSTRAINT subscriptions_client_id_fkey,
    ADD CONSTRAINT subscriptions_client_id_fkey
        FOREIGN KEY (client_id) REFERENCES users(id);

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'payments'
    ) THEN
        ALTER TABLE payments
            DROP CONSTRAINT payments_payer_id_fkey,
            ADD CONSTRAINT payments_payer_id_fkey
                FOREIGN KEY (payer_id) REFERENCES users(id);
    END IF;
END $$;
-- +goose StatementEnd
