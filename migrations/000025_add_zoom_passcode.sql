-- +goose Up
ALTER TABLE discovery_bookings
    ADD COLUMN IF NOT EXISTS zoom_passcode VARCHAR;

-- zoom_meeting_link and zoom_meeting_id on bookings may not exist yet (added by
-- reschedule-paid-session branch); add them all here with IF NOT EXISTS.
ALTER TABLE bookings
    ADD COLUMN IF NOT EXISTS zoom_meeting_link VARCHAR,
    ADD COLUMN IF NOT EXISTS zoom_meeting_id   VARCHAR,
    ADD COLUMN IF NOT EXISTS zoom_passcode     VARCHAR;

-- +goose Down
ALTER TABLE discovery_bookings
    DROP COLUMN IF EXISTS zoom_passcode;

-- Only drop zoom_passcode here; zoom_meeting_link and zoom_meeting_id may be
-- owned by a separate migration and must not be dropped by this rollback.
ALTER TABLE bookings
    DROP COLUMN IF EXISTS zoom_passcode;
