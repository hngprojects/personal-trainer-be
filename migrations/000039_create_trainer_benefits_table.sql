-- +goose Up
-- trainer_benefits holds the marketing-style "what you get working with this
-- trainer" copy displayed on the trainer profile page. Each benefit is a
-- title + subtext pair, and a trainer has 0..N of them (no enforced upper
-- bound at the DB layer; the handler can apply a sane cap if product asks).
--
-- Modeled as a separate table rather than a JSON column on trainers because
-- the front-end renders them as a list with per-item positioning, and
-- ordering changes shouldn't rewrite the whole trainer row. Each row owns
-- its own UUID so the future "edit one benefit" / "delete one benefit"
-- endpoints have a stable handle.
CREATE TABLE IF NOT EXISTS trainer_benefits (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    trainer_id  UUID         NOT NULL REFERENCES trainers(id) ON DELETE CASCADE,
    -- position is the display order, 1-indexed. The handler inserts with
    -- explicit positions starting at 1; clients can reorder by issuing
    -- per-row updates. Gaps on delete are intentional (no auto-renumber).
    -- CHECK enforces the 1-indexing contract — a 0 or negative position
    -- would otherwise satisfy the unique index but break any client that
    -- assumes positive ordering.
    position    INTEGER      NOT NULL CHECK (position > 0),
    title       TEXT         NOT NULL,
    subtext     TEXT         NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_trainer_benefits_trainer_id
    ON trainer_benefits (trainer_id);

-- (trainer_id, position) uniqueness prevents two benefits rendering at the
-- same slot. Same defensive pattern as trainer_images.
CREATE UNIQUE INDEX IF NOT EXISTS idx_trainer_benefits_trainer_id_position
    ON trainer_benefits (trainer_id, position);


-- +goose Down
DROP INDEX IF EXISTS idx_trainer_benefits_trainer_id_position;
DROP INDEX IF EXISTS idx_trainer_benefits_trainer_id;
DROP TABLE IF EXISTS trainer_benefits;
