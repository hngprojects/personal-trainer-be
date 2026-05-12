-- +goose Up
CREATE TABLE IF NOT EXISTS trainer_invite_tokens (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token       TEXT NOT NULL UNIQUE,
  expires_at  TIMESTAMPTZ NOT NULL,
  used_at     TIMESTAMPTZ NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_trainer_invite_tokens_user_id ON trainer_invite_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_trainer_invite_tokens_token ON trainer_invite_tokens(token);

-- +goose Down
DROP TABLE IF EXISTS trainer_invite_tokens;
