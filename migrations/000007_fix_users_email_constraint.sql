-- +goose Up
ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_email_key;

ALTER TABLE users
    ADD CONSTRAINT users_email_auth_provider_key
    UNIQUE (email, auth_provider);

-- +goose Down
ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_email_auth_provider_key;

ALTER TABLE users
    ADD CONSTRAINT users_email_key UNIQUE (email);