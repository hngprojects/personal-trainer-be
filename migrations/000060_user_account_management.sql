-- +goose Up
-- No schema changes needed:
-- - Soft deactivation uses the existing users.is_active column (already present)
-- - Hard delete cascades via existing FK constraints
-- - Active sessions filter uses existing booking_status column

-- +goose Down
-- No schema changes to roll back
