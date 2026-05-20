-- +goose Up
-- Trainer specializations are now multi-valued. A trainer can specialise in
-- any non-empty subset of the canonical 5-value catalog (yoga, speed, cardio,
-- endurance, strength). We model this as a TEXT[] on the trainers row rather
-- than a join table because:
--   - the catalog is fixed and small; we don't need a separate lookup table
--     to hold values that almost never change
--   - GIN indexes make `WHERE specializations @> ARRAY['yoga']` filter as
--     fast as the old equality lookup
--   - a single column round-trips trivially through sqlc
--
-- The catalog is enforced by CHECK (specializations <@ allowed_set). Adding
-- a 6th value later is one ALTER … DROP / ADD CONSTRAINT away.
--
-- We drop the old TEXT column outright rather than dual-writing. Confirmed
-- with the team: no production rows depend on existing specialization values
-- (project is still pre-launch on this surface). Any test rows lose their
-- value and need to be re-set by the admin.

DROP INDEX IF EXISTS idx_trainers_specialization;

ALTER TABLE trainers DROP COLUMN IF EXISTS specialization;

ALTER TABLE trainers
    ADD COLUMN specializations TEXT[] NOT NULL DEFAULT '{}';

-- Enforce the catalog: every element of specializations must be one of the
-- 5 allowed values, AND the array must hold 1..5 items. The default '{}'
-- above seeds existing rows; the CHECK allows '{}' but the application
-- handler requires at least one on create.
ALTER TABLE trainers
    ADD CONSTRAINT trainers_specializations_catalog_chk
    CHECK (
        specializations <@ ARRAY['yoga','speed','cardio','endurance','strength']::text[]
        AND cardinality(specializations) BETWEEN 0 AND 5
    );

-- GIN index for containment queries (`specializations @> ARRAY[$1]`) used by
-- the public list/filter endpoint. Equality `=` on the old TEXT column had a
-- btree; the array equivalent is GIN.
CREATE INDEX IF NOT EXISTS idx_trainers_specializations_gin
    ON trainers USING GIN (specializations);


-- +goose Down
DROP INDEX IF EXISTS idx_trainers_specializations_gin;
ALTER TABLE trainers DROP CONSTRAINT IF EXISTS trainers_specializations_catalog_chk;
ALTER TABLE trainers DROP COLUMN IF EXISTS specializations;

ALTER TABLE trainers ADD COLUMN specialization TEXT;
CREATE INDEX IF NOT EXISTS idx_trainers_specialization ON trainers (specialization);
