-- +goose Up
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS gender       TEXT,
    ADD COLUMN IF NOT EXISTS fitness_goals TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS fitness_level TEXT,
    ADD COLUMN IF NOT EXISTS avatar_url   TEXT;

-- +goose Down
ALTER TABLE users
    DROP COLUMN IF EXISTS gender,
    DROP COLUMN IF EXISTS fitness_goals,
    DROP COLUMN IF EXISTS fitness_level,
    DROP COLUMN IF EXISTS avatar_url;
