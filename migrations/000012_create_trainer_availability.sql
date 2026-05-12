-- +goose Up
CREATE TABLE IF NOT EXISTS trainer_availability (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  trainer_id  UUID NOT NULL REFERENCES trainers(id) ON DELETE CASCADE,
  day_of_week INT  NOT NULL CHECK (day_of_week >= 0 AND day_of_week <= 6),
  start_time  TIME NOT NULL,
  end_time    TIME NOT NULL,
  timezone    TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK (end_time > start_time)
);

CREATE INDEX IF NOT EXISTS idx_trainer_availability_trainer_day
  ON trainer_availability(trainer_id, day_of_week);

-- +goose Down
DROP TABLE IF EXISTS trainer_availability;