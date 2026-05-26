-- +goose Up
-- Stores a per-user Zoom OAuth grant so the booking flow can create
-- meetings on the user's OWN Zoom account (trainer-hosted mode) rather
-- than on the org's server-to-server account. Keyed on user_id so a
-- user can only ever have one connection at a time — reconnecting
-- overwrites the row.
--
-- Tokens are stored ENCRYPTED. The handler encrypts using AES-GCM
-- with a key from ZOOM_TOKEN_ENCRYPTION_KEY before INSERT and decrypts
-- on read; the raw access_token / refresh_token bytes never sit in
-- the column. Without this, a DB leak hands every trainer's Zoom
-- account to the attacker — Zoom refresh tokens are long-lived (60
-- days even without use) and let the holder create meetings, access
-- recordings, and read participant lists.
CREATE TABLE IF NOT EXISTS user_zoom_credentials (
    user_id              UUID         PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    -- Both fields hold the AES-GCM ciphertext + nonce + tag, base64
    -- encoded. NOT NULL because we never persist a partial credential
    -- pair (success path writes both, failure path writes nothing).
    access_token_enc     TEXT         NOT NULL,
    refresh_token_enc    TEXT         NOT NULL,
    -- access_token_expires_at is the wall-clock time the access token
    -- becomes unusable. Provider code checks this before each Zoom
    -- API call; if past, refreshes synchronously using refresh_token.
    access_token_expires_at TIMESTAMPTZ NOT NULL,
    scope                TEXT         NOT NULL,
    -- Zoom's own user identifier (their numeric/UUID id). Stored so
    -- audit logs can correlate our user with what Zoom sees, and so
    -- we can call /users/{zoom_user_id}/meetings without an extra
    -- round trip to /users/me.
    zoom_user_id         TEXT         NOT NULL,
    zoom_account_id      TEXT,        -- nullable; not all OAuth grants carry it
    zoom_email           TEXT,        -- nullable; surfaced in the "Connected as foo@bar" UI
    connected_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    -- Last successful API call against Zoom for this user. Lets ops
    -- spot stale connections (e.g. trainer revoked the grant on the
    -- Zoom side; first failed call updates last_failure_at, this
    -- field never advances).
    last_success_at      TIMESTAMPTZ,
    last_failure_at      TIMESTAMPTZ,
    last_failure_reason  TEXT
);

-- expires_at index powers the (future) background refresh job that
-- proactively renews tokens nearing expiry — cheap to add now since
-- the column is already indexed for the lookup.
CREATE INDEX IF NOT EXISTS idx_user_zoom_credentials_expires_at
    ON user_zoom_credentials(access_token_expires_at);

-- +goose Down
DROP INDEX IF EXISTS idx_user_zoom_credentials_expires_at;
DROP TABLE IF EXISTS user_zoom_credentials;
