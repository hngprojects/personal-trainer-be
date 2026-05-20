-- +goose Up
-- trainer_images holds the gallery shots for each trainer. Separate from the
-- main trainers row so a trainer can have 0..N images without padding the
-- trainers row with image_1..image_5 columns (rigid and ugly) or stuffing
-- a JSON array (no FK integrity, harder to query individually).
--
-- The 5-image cap is enforced in the application layer (admin handler counts
-- existing rows + incoming uploads). We don't enforce it at the DB layer
-- because a CHECK constraint on the count would require a trigger or a
-- DEFERRABLE row count, both of which add complexity for marginal benefit.
CREATE TABLE IF NOT EXISTS trainer_images (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    trainer_id  UUID         NOT NULL REFERENCES trainers(id) ON DELETE CASCADE,
    image_url   TEXT         NOT NULL,
    -- position is the display ordering. Insert with MAX(position)+1; on
    -- delete we leave gaps (no auto-renumber) because cardinality is small
    -- and clients can sort client-side.
    position    INTEGER      NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_trainer_images_trainer_id
    ON trainer_images (trainer_id);

-- A trainer can't have two images at the same position (defensive — the
-- handler uses MAX(position)+1 inside a per-trainer advisory lock so
-- collisions shouldn't happen, but the constraint catches bugs early
-- instead of letting two images render at "position 3" forever).
CREATE UNIQUE INDEX IF NOT EXISTS idx_trainer_images_trainer_id_position
    ON trainer_images (trainer_id, position);

-- +goose StatementBegin
-- DB-level enforcement of the 5-images-per-trainer cap. The app layer
-- counts before enqueuing for a fast-fail UX, but with async workers and
-- concurrent requests an app-level check is not race-safe. This trigger
-- is the authoritative guard — no caller (including future buggy ones)
-- can bypass it.
--
-- Race-safety: the AddTrainerImage query takes pg_advisory_xact_lock on
-- a per-trainer key BEFORE this trigger runs, so concurrent inserts for
-- the SAME trainer are serialised; the count below is always current.
CREATE OR REPLACE FUNCTION enforce_trainer_image_cap() RETURNS TRIGGER AS $$
BEGIN
    IF (SELECT COUNT(*) FROM trainer_images WHERE trainer_id = NEW.trainer_id) >= 5 THEN
        RAISE EXCEPTION 'trainer_images: maximum 5 images per trainer (trainer_id=%)', NEW.trainer_id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER enforce_trainer_image_cap_trg
    BEFORE INSERT ON trainer_images
    FOR EACH ROW EXECUTE FUNCTION enforce_trainer_image_cap();

-- +goose Down
DROP TRIGGER IF EXISTS enforce_trainer_image_cap_trg ON trainer_images;
DROP FUNCTION IF EXISTS enforce_trainer_image_cap();
DROP INDEX IF EXISTS idx_trainer_images_trainer_id_position;
DROP INDEX IF EXISTS idx_trainer_images_trainer_id;
DROP TABLE IF EXISTS trainer_images;
