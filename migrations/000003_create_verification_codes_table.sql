-- +goose Up
CREATE TABLE IF NOT EXISTS verification_codes (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT        NOT NULL,
    code       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_verification_codes_email ON verification_codes(email);

-- +goose Down
DROP INDEX IF EXISTS idx_verification_codes_email;
DROP TABLE IF EXISTS verification_codes;
