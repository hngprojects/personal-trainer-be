-- +goose Up
CREATE TABLE IF NOT EXISTS booking_slots (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    trainer_id UUID        NOT NULL REFERENCES trainers(id) ON DELETE CASCADE,
    starts_at  TIMESTAMPTZ NOT NULL,
    ends_at    TIMESTAMPTZ NOT NULL,
    timezone   VARCHAR     NOT NULL DEFAULT 'UTC',
    status     VARCHAR     NOT NULL DEFAULT 'available'
        CHECK (status IN ('available', 'locked', 'booked', 'unavailable')),
    locked_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    locked_at  TIMESTAMPTZ,
    booking_id UUID REFERENCES bookings(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT booking_slots_time_window_check CHECK (ends_at > starts_at)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_booking_slots_booking_id_unique
    ON booking_slots(booking_id)
    WHERE booking_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_booking_slots_trainer_start
    ON booking_slots(trainer_id, starts_at);
CREATE INDEX IF NOT EXISTS idx_booking_slots_trainer_status
    ON booking_slots(trainer_id, status);

CREATE TABLE IF NOT EXISTS booking_reschedule_history (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id     UUID        NOT NULL REFERENCES bookings(id) ON DELETE CASCADE,
    old_slot_id    UUID REFERENCES booking_slots(id) ON DELETE SET NULL,
    new_slot_id    UUID REFERENCES booking_slots(id) ON DELETE SET NULL,
    reason         TEXT,
    rescheduled_by UUID        NOT NULL REFERENCES users(id),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_booking_reschedule_history_booking_id
    ON booking_reschedule_history(booking_id);
CREATE INDEX IF NOT EXISTS idx_booking_reschedule_history_rescheduled_by
    ON booking_reschedule_history(rescheduled_by);
CREATE INDEX IF NOT EXISTS idx_booking_reschedule_history_created_at
    ON booking_reschedule_history(created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_booking_reschedule_history_created_at;
DROP INDEX IF EXISTS idx_booking_reschedule_history_rescheduled_by;
DROP INDEX IF EXISTS idx_booking_reschedule_history_booking_id;
DROP TABLE IF EXISTS booking_reschedule_history;

DROP INDEX IF EXISTS idx_booking_slots_trainer_status;
DROP INDEX IF EXISTS idx_booking_slots_trainer_start;
DROP INDEX IF EXISTS idx_booking_slots_booking_id_unique;
DROP TABLE IF EXISTS booking_slots;
