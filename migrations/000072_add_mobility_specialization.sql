-- +goose Up
-- Drop the hardcoded specializations CHECK constraint so the catalog is
-- managed via the categories table instead of a schema change.
-- Validation now happens in Go by querying categories.slug before saving.
ALTER TABLE trainers DROP CONSTRAINT IF EXISTS trainers_specializations_catalog_chk;

-- +goose Down
-- Restore the original 5-value constraint.
ALTER TABLE trainers
    ADD CONSTRAINT trainers_specializations_catalog_chk
    CHECK (
        specializations <@ ARRAY['yoga','speed','cardio','endurance','strength']::text[]
        AND cardinality(specializations) BETWEEN 0 AND 5
    );
