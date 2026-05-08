-- +goose Up
CREATE TABLE IF NOT EXISTS trainers (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,

    specialization       TEXT,
    bio                  TEXT,
    years_of_experience  INT,

    intro_video_url      TEXT,
    display_picture      TEXT,

    calendly_connected   BOOLEAN NOT NULL DEFAULT false,
    calendly_link        TEXT,

    onboarding_status    TEXT NOT NULL DEFAULT 'pending' -- pending, approved, rejected, suspended
        CHECK (onboarding_status IN ('pending', 'approved', 'rejected', 'suspended')),

    average_rating       NUMERIC,
    total_reviews        INT NOT NULL DEFAULT 0,

    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_trainers_specialization ON trainers(specialization);
CREATE INDEX IF NOT EXISTS idx_trainers_onboarding_status ON trainers(onboarding_status);

-- +goose Down
DROP TABLE IF EXISTS trainers;
DROP INDEX IF EXISTS idx_trainers_specialization;
DROP INDEX IF EXISTS idx_trainers_onboarding_status;