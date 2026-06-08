-- +goose Up
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS phone_number TEXT;

-- +goose Down
ALTER TABLE bookings DROP COLUMN IF EXISTS phone_number;