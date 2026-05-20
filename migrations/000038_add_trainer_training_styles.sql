-- +goose Up
-- Training styles are short free-text tags (e.g. "calisthenics", "HIIT",
-- "yoga") that describe how a trainer works with clients. Unlike
-- specializations (a fixed 5-value catalog), training styles are open-ended
-- because trends keep shifting and admins want to set them per trainer
-- without a code change.
--
-- Cap at 4 to match the product spec — the cap is enforced both here (DB
-- safety net) and in the handler (clean 400 error). Empty array is allowed
-- so existing rows don't need backfill.

ALTER TABLE trainers
    ADD COLUMN training_styles TEXT[] NOT NULL DEFAULT '{}';

ALTER TABLE trainers
    ADD CONSTRAINT trainers_training_styles_cardinality_chk
    CHECK (cardinality(training_styles) BETWEEN 0 AND 4);


-- +goose Down
ALTER TABLE trainers DROP CONSTRAINT IF EXISTS trainers_training_styles_cardinality_chk;
ALTER TABLE trainers DROP COLUMN IF EXISTS training_styles;
