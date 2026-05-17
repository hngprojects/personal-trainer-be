-- +goose Up
ALTER TABLE bookings
    ADD COLUMN IF NOT EXISTS zoom_meeting_link VARCHAR,
    ADD COLUMN IF NOT EXISTS zoom_meeting_id   VARCHAR,
    ADD COLUMN IF NOT EXISTS reschedule_count  INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS paid_booking_reschedule_history (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id      UUID NOT NULL REFERENCES bookings(id) ON DELETE CASCADE,
    previous_start  TIMESTAMPTZ NOT NULL,
    new_start       TIMESTAMPTZ NOT NULL,
    reason          TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_paid_reschedule_history_booking_id
    ON paid_booking_reschedule_history(booking_id);

-- +goose Down
DROP INDEX IF EXISTS idx_paid_reschedule_history_booking_id;
DROP TABLE IF EXISTS paid_booking_reschedule_history;
ALTER TABLE bookings
    DROP COLUMN IF EXISTS zoom_meeting_link,
    DROP COLUMN IF EXISTS zoom_meeting_id,
    DROP COLUMN IF EXISTS reschedule_count;
