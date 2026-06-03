-- +goose Up
-- Add ON DELETE CASCADE to FK constraints that block hard-deleting a user.
-- bookings, subscriptions, and payments were missing CASCADE — the others
-- already had it from their original migrations.

ALTER TABLE bookings
    DROP CONSTRAINT bookings_client_id_fkey,
    ADD CONSTRAINT bookings_client_id_fkey
        FOREIGN KEY (client_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE subscriptions
    DROP CONSTRAINT subscriptions_client_id_fkey,
    ADD CONSTRAINT subscriptions_client_id_fkey
        FOREIGN KEY (client_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE payments
    DROP CONSTRAINT payments_payer_id_fkey,
    ADD CONSTRAINT payments_payer_id_fkey
        FOREIGN KEY (payer_id) REFERENCES users(id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE bookings
    DROP CONSTRAINT bookings_client_id_fkey,
    ADD CONSTRAINT bookings_client_id_fkey
        FOREIGN KEY (client_id) REFERENCES users(id);

ALTER TABLE subscriptions
    DROP CONSTRAINT subscriptions_client_id_fkey,
    ADD CONSTRAINT subscriptions_client_id_fkey
        FOREIGN KEY (client_id) REFERENCES users(id);

ALTER TABLE payments
    DROP CONSTRAINT payments_payer_id_fkey,
    ADD CONSTRAINT payments_payer_id_fkey
        FOREIGN KEY (payer_id) REFERENCES users(id);
