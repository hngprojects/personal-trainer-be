-- +goose Up
CREATE TABLE IF NOT EXISTS booking_slots (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    trainer_id  UUID        NOT NULL REFERENCES trainers(id),
    day_of_week SMALLINT    NOT NULL CHECK (day_of_week BETWEEN 0 AND 6), -- 0=Sun, 6=Sat
    start_time  TIME        NOT NULL,
    end_time    TIME        NOT NULL,
    timezone    TEXT        NOT NULL DEFAULT 'Africa/Lagos',
    is_active   BOOLEAN     NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT booking_slots_valid_range CHECK (end_time > start_time)
);

CREATE INDEX IF NOT EXISTS idx_booking_slots_day ON booking_slots(day_of_week);
CREATE INDEX IF NOT EXISTS idx_booking_slots_active ON booking_slots(is_active);

-- +goose Down
DROP INDEX IF EXISTS idx_booking_slots_active;
DROP INDEX IF EXISTS idx_booking_slots_day;
DROP TABLE IF EXISTS booking_slots;
