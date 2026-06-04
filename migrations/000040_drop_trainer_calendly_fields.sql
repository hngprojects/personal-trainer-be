-- +goose Up
-- Calendly integration is being removed from the trainer profile. Drop the
-- two columns that backed it: `calendly_connected` (whether the trainer had
-- linked their Calendly account) and `calendly_link` (the public URL). No
-- application code reads them anymore after the admin-creates-trainer
-- redesign — the booking flow uses our own scheduling tables, not Calendly.
ALTER TABLE trainers DROP COLUMN IF EXISTS calendly_link;
ALTER TABLE trainers DROP COLUMN IF EXISTS calendly_connected;

-- +goose Down
ALTER TABLE trainers ADD COLUMN calendly_connected BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE trainers ADD COLUMN calendly_link      TEXT;
