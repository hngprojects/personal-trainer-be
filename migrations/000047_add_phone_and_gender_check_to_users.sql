-- +goose Up
-- Add users.phone_number for the new trainer-create flow (POST /trainers
-- accepts phone + gender). Nullable: existing rows stay NULL and any
-- caller that omits it on create is fine.
ALTER TABLE users ADD COLUMN IF NOT EXISTS phone_number TEXT;

-- users.gender already existed without a CHECK; the trainer-create
-- handler now writes one of a fixed enum, so lock that down at the DB
-- level too. Normalise any pre-existing non-conforming values to NULL
-- first so the constraint can apply cleanly (the column was loosely
-- typed before, so a stray value would otherwise fail the ALTER).
UPDATE users
SET gender = NULL
WHERE gender IS NOT NULL
  AND gender NOT IN ('male', 'female', 'other', 'prefer_not_to_say');

ALTER TABLE users
    ADD CONSTRAINT users_gender_valid
    CHECK (gender IS NULL OR gender IN ('male', 'female', 'other', 'prefer_not_to_say'));

-- +goose Down
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_gender_valid;
ALTER TABLE users DROP COLUMN IF EXISTS phone_number;
