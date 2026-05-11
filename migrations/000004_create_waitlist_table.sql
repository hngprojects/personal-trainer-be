-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE TABLE IF NOT EXISTS waitlist (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email      VARCHAR(255)        NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    phone_number VARCHAR(20),
    location VARCHAR(255),
    name VARCHAR(255)
);

-- +goose Down
DROP TABLE IF EXISTS waitlist;
