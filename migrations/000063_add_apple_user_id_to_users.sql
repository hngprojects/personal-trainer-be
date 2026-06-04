-- +goose Up
-- Apple Sign In returns a stable `sub` claim on every identity token,
-- but the `email` claim is ONLY present on the first authorization.
-- Looking up the user by (email, auth_provider) — which works for
-- Google — therefore fails on every subsequent sign-in. Store the
-- sub explicitly and look up by it instead.
--
-- Nullable because existing rows (Google, local, trainer/admin
-- provisioning) don't have one. UNIQUE so the same Apple account can't
-- map to two user rows after a botched data import.

ALTER TABLE users ADD COLUMN IF NOT EXISTS apple_user_id TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS users_apple_user_id_key
    ON users (apple_user_id)
    WHERE apple_user_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS users_apple_user_id_key;
ALTER TABLE users DROP COLUMN IF EXISTS apple_user_id;
