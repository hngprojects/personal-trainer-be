-- name: GetAdminSettings :one
-- Reads the singleton row. The migration seeds it on first apply, so
-- a missing row here is a hard error worth surfacing — never expected
-- in normal operation.
SELECT
    id,
    default_session_duration_min,
    max_trainers_displayed,
    require_video_before_listing,
    auto_assign_trainer,
    updated_at
FROM admin_settings
WHERE singleton_lock = 'singleton'
LIMIT 1;

-- name: UpdateAdminSettings :one
-- Partial-update via COALESCE: pass NULL for any field the caller
-- doesn't want to change, the existing value is kept. Lets the FE
-- submit only the toggled rows from the settings form rather than
-- having to re-send every field.
UPDATE admin_settings
SET
    default_session_duration_min = COALESCE(sqlc.narg('default_session_duration_min')::int,  default_session_duration_min),
    max_trainers_displayed       = COALESCE(sqlc.narg('max_trainers_displayed')::int,        max_trainers_displayed),
    require_video_before_listing = COALESCE(sqlc.narg('require_video_before_listing')::bool, require_video_before_listing),
    auto_assign_trainer          = COALESCE(sqlc.narg('auto_assign_trainer')::bool,          auto_assign_trainer),
    updated_at                   = NOW()
WHERE singleton_lock = 'singleton'
RETURNING
    id,
    default_session_duration_min,
    max_trainers_displayed,
    require_video_before_listing,
    auto_assign_trainer,
    updated_at;
