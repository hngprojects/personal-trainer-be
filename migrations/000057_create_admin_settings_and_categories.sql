-- +goose Up

-- Single-row config the admin settings page edits. We enforce
-- "exactly one row" via a UNIQUE constant column rather than relying
-- on application code to never insert a second row; a stray second
-- insert would otherwise silently make GET ambiguous. Updates target
-- the well-known id so the handler doesn't have to read the row first.
CREATE TABLE IF NOT EXISTS admin_settings (
    -- Stable, well-known id. Never changes. The handler reads/writes
    -- this row by id.
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Single-row guard. Any second INSERT trips the unique on
    -- singleton_lock. Default 'singleton' makes inserts opaque to the
    -- caller — they don't have to supply the value.
    singleton_lock              TEXT NOT NULL UNIQUE DEFAULT 'singleton',

    -- General defaults.
    default_session_duration_min INTEGER NOT NULL DEFAULT 60
        CHECK (default_session_duration_min BETWEEN 5 AND 480),
    max_trainers_displayed       INTEGER NOT NULL DEFAULT 6
        CHECK (max_trainers_displayed BETWEEN 1 AND 100),

    -- Trainer rules.
    require_video_before_listing BOOLEAN NOT NULL DEFAULT TRUE,
    auto_assign_trainer          BOOLEAN NOT NULL DEFAULT FALSE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed the singleton row. Idempotent — re-running this migration on a
-- DB that already has the row is a no-op.
INSERT INTO admin_settings (singleton_lock)
VALUES ('singleton')
ON CONFLICT (singleton_lock) DO NOTHING;

-- Client-facing specialty catalog. The admin can add/remove via the
-- settings page; the user-side /categories endpoint reads from it.
--
-- NOT joined to trainers.specializations YET — that column still has
-- its hardcoded CHECK constraint from migration 000037. Migrating
-- trainers to reference this table (so adding "HIIT" here lets a
-- trainer pick it without a schema change) is intentionally a follow-
-- up so this PR stays focused on the settings page itself.
CREATE TABLE IF NOT EXISTS categories (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- name is the display label ("Strength"). Trimmed at insert time
    -- but not lowercased — preserves the admin's casing for the UI.
    name        TEXT NOT NULL UNIQUE,
    -- slug is the URL-safe / programmatic key ("strength"). Useful
    -- when we later wire trainer signup to look up categories by slug
    -- rather than by display name.
    slug        TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed with the seven categories shown on the admin settings mockup
-- so the page renders something on first boot in any environment.
-- ON CONFLICT keeps re-runs idempotent.
INSERT INTO categories (name, slug) VALUES
    ('Strength',    'strength'),
    ('Yoga',        'yoga'),
    ('HIIT',        'hiit'),
    ('Pilates',     'pilates'),
    ('Endurance',   'endurance'),
    ('Weight loss', 'weight-loss'),
    ('Mobility',    'mobility')
ON CONFLICT (slug) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS admin_settings;
