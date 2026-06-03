-- +goose Up
-- Extend discovery_bookings to support whatsapp and messenger in addition
-- to the original zoom_meeting and phone_callback modes.
-- Also adds messenger_username column for Facebook Messenger bookings.
ALTER TABLE discovery_bookings
    DROP CONSTRAINT IF EXISTS discovery_bookings_contact_mode_check,
    ADD CONSTRAINT discovery_bookings_contact_mode_check
        CHECK (contact_mode IN ('zoom_meeting', 'phone_callback', 'whatsapp', 'messenger'));

ALTER TABLE discovery_bookings
    ADD COLUMN IF NOT EXISTS messenger_username TEXT;

-- +goose Down
ALTER TABLE discovery_bookings
    DROP COLUMN IF EXISTS messenger_username;

ALTER TABLE discovery_bookings
    DROP CONSTRAINT IF EXISTS discovery_bookings_contact_mode_check,
    ADD CONSTRAINT discovery_bookings_contact_mode_check
        CHECK (contact_mode IN ('zoom_meeting', 'phone_callback'));
