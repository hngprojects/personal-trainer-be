-- +goose Up
ALTER TABLE bookings
ADD COLUMN IF NOT EXISTS zoom_meeting_link VARCHAR(255),
ADD COLUMN IF NOT EXISTS zoom_meeting_id VARCHAR(255),
ADD COLUMN IF NOT EXISTS reschedule_count INT;

-- +goose Down
ALTER TABLE bookings
DROP COLUMN IF EXISTS zoom_meeting_link,
DROP COLUMN IF EXISTS zoom_meeting_id,
DROP COLUMN IF EXISTS reschedule_count;
