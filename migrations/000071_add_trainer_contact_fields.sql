-- +goose Up
ALTER TABLE trainers
  ADD COLUMN IF NOT EXISTS whatsapp_number  TEXT,
  ADD COLUMN IF NOT EXISTS apple_id         TEXT,
  ADD COLUMN IF NOT EXISTS messenger_handle TEXT;

-- +goose Down
ALTER TABLE trainers
  DROP COLUMN IF EXISTS whatsapp_number,
  DROP COLUMN IF EXISTS apple_id,
  DROP COLUMN IF EXISTS messenger_handle;
