-- +goose Up
-- Apple SIWA refresh token, encrypted at rest with AES-256-GCM.
-- Captured at sign-in time when the mobile client sends the
-- authorization_code alongside the identity_token, and consumed by
-- the account-deletion handler to call Apple's /auth/revoke endpoint
-- (Apple Review Guideline 5.1.1 (v) compliance for account deletion).
--
-- Nullable because:
--   - Pre-existing apple users predate this column.
--   - The mobile client may roll out the authorization_code change in
--     a later release; users who signed in before that update will
--     never have a refresh token. Their account deletion still works,
--     it just skips the revoke step.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS apple_refresh_token_enc TEXT;

-- +goose Down
ALTER TABLE users
    DROP COLUMN IF EXISTS apple_refresh_token_enc;
