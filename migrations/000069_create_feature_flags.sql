-- +goose Up
-- Generic key-value boolean feature flags table. Used to drive
-- runtime kill-switches that the admin can flip without a deploy:
-- payments enable/disable, beta features behind a gate, etc.
--
-- Why a dedicated table instead of extending admin_settings: the
-- admin_settings table is a single-row singleton with a known
-- column-per-setting shape. Feature flags are open-ended — we'll
-- add more keys over time (payment_enabled, push_notifications_enabled,
-- waitlist_open, ...) and we want the read path to stay a single
-- query no matter how many we have.
CREATE TABLE IF NOT EXISTS feature_flags (
    -- Stable, human-readable identifier. Read by the public endpoint
    -- so the value here is part of the API contract — pick names that
    -- read well in JSON: snake_case, descriptive, boolean-shaped.
    key         TEXT PRIMARY KEY,

    -- The actual flag value. NOT NULL — flags are always on or off.
    -- New flags should add a row with a sensible default rather than
    -- relying on absence-equals-false semantics in the read path.
    enabled     BOOLEAN NOT NULL DEFAULT FALSE,

    -- Audit columns so an operator can answer "who turned payments off
    -- yesterday?" without spelunking through application logs. SET NULL
    -- on user deletion preserves the audit trail of the flip — losing
    -- who-did-it is preferable to losing the row entirely.
    updated_by  UUID REFERENCES users(id) ON DELETE SET NULL,

    -- Optional free-text rationale. The admin endpoint accepts a
    -- `notes` field and stores it here; useful for incident timelines
    -- ("disabling payments due to merchant outage 2026-06-10 14:32").
    notes       TEXT,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed the payment flag DISABLED by default. Production deploys will
-- explicitly flip this on once the IAP backend is fully tested; that
-- way a fresh environment doesn't accidentally accept purchases
-- before the operator has verified everything is wired up.
INSERT INTO feature_flags (key, enabled, notes)
VALUES ('payment_enabled', FALSE, 'Initial seed — flip on once IAP is verified end-to-end')
ON CONFLICT (key) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS feature_flags;
