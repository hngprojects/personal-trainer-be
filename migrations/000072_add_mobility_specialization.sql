-- +goose Up
-- Drop the hardcoded specializations CHECK constraint so the catalog is
-- managed via the categories table instead of a schema change.
-- Validation now happens in Go by querying categories.slug before saving.
ALTER TABLE trainers DROP CONSTRAINT IF EXISTS trainers_specializations_catalog_chk;

-- +goose Down
-- NOTE: This rollback is intentionally a no-op.
-- Restoring the original 5-value CHECK constraint after this migration has run
-- would fail if any trainer row already holds a slug outside that set
-- (e.g. "mobility", "hiit") — PostgreSQL will reject the ADD CONSTRAINT.
-- Rolling back requires a manual data audit first. Leave the constraint absent
-- and roll back the application code separately if needed.
SELECT 1; -- no-op placeholder required by goose
