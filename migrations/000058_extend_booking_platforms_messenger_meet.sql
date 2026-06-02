-- +goose Up

-- Widen `bookings.session_platform` to add `messenger` and drop the
-- dead `whatsapp` value (in the CHECK since migration 000012 but no
-- handler ever implemented it). `google_meet` is already accepted by
-- the existing CHECK so it stays.
ALTER TABLE bookings DROP CONSTRAINT IF EXISTS bookings_session_platform_check;
ALTER TABLE bookings
    ADD CONSTRAINT bookings_session_platform_check
    CHECK (session_platform IN ('zoom', 'google_meet', 'messenger'));

-- messenger_handle holds whatever the client typed in (their Facebook
-- profile URL, m.me link, or numeric ID). Free-form text by design —
-- Facebook handle formats are heterogeneous and we never try to
-- validate or call out to FB. The trainer sees this verbatim and
-- decides what to do with it.
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS messenger_handle TEXT;

-- Widen discovery_bookings.contact_mode to mirror the bookings table:
-- the original migration (000018) only allowed `zoom_meeting` and
-- `phone_callback`. Adding the two new options keeps the discovery
-- funnel symmetric with the paid-booking experience.
ALTER TABLE discovery_bookings DROP CONSTRAINT IF EXISTS discovery_bookings_contact_mode_check;
ALTER TABLE discovery_bookings
    ADD CONSTRAINT discovery_bookings_contact_mode_check
    CHECK (contact_mode IN ('zoom_meeting', 'phone_callback', 'google_meet', 'messenger'));

ALTER TABLE discovery_bookings ADD COLUMN IF NOT EXISTS messenger_handle TEXT;

-- +goose Down

ALTER TABLE discovery_bookings DROP COLUMN IF EXISTS messenger_handle;
ALTER TABLE discovery_bookings DROP CONSTRAINT IF EXISTS discovery_bookings_contact_mode_check;
ALTER TABLE discovery_bookings
    ADD CONSTRAINT discovery_bookings_contact_mode_check
    CHECK (contact_mode IN ('zoom_meeting', 'phone_callback'));

ALTER TABLE bookings DROP COLUMN IF EXISTS messenger_handle;
ALTER TABLE bookings DROP CONSTRAINT IF EXISTS bookings_session_platform_check;
ALTER TABLE bookings
    ADD CONSTRAINT bookings_session_platform_check
    CHECK (session_platform IN ('whatsapp', 'google_meet', 'zoom'));
