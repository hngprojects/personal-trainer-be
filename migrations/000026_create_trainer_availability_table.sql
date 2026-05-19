-- +goose Up
CREATE TABLE IF NOT EXISTS trainer_availability (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    trainer_id  UUID        NOT NULL REFERENCES trainers(id) ON DELETE CASCADE,
    day_of_week SMALLINT    NOT NULL CHECK (day_of_week BETWEEN 0 AND 6), -- 0=Sun, 6=Sat
    start_time  TIME        NOT NULL,
    end_time    TIME        NOT NULL,
    timezone    TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT trainer_availability_valid_range CHECK (end_time > start_time)
);

CREATE INDEX IF NOT EXISTS idx_trainer_availability_trainer ON trainer_availability(trainer_id);
CREATE INDEX IF NOT EXISTS idx_trainer_availability_day    ON trainer_availability(day_of_week);
CREATE UNIQUE INDEX IF NOT EXISTS idx_trainer_availability_unique ON trainer_availability(trainer_id, day_of_week, start_time, end_time);

-- +goose Down
DROP INDEX IF EXISTS idx_trainer_availability_unique;
DROP INDEX IF EXISTS idx_trainer_availability_day;
DROP INDEX IF EXISTS idx_trainer_availability_trainer;
DROP TABLE IF EXISTS trainer_availability;