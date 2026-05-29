-- name: GetRoleByName :one
SELECT * FROM roles WHERE name = $1 LIMIT 1;

-- name: AssignUserRole :exec
INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)
ON CONFLICT (user_id, role_id) DO NOTHING;

-- name: UserHasRole :one
SELECT EXISTS (
    SELECT 1 FROM user_roles ur
    INNER JOIN roles r ON r.id = ur.role_id
    WHERE ur.user_id = $1 AND r.name = $2
) AS has_role;

-- name: GetUserRoles :many
SELECT r.id, r.name, r.created_at
FROM roles r
INNER JOIN user_roles ur ON ur.role_id = r.id
WHERE ur.user_id = $1;

-- name: ListAdminUserIDs :many
-- Returns every user holding the admin OR super_admin role. Used by
-- the notification broadcast helper so a single system event (a new
-- discovery booking, a subscription cancel, etc.) can fan out to all
-- staff without the caller having to know who they are.
--
-- DISTINCT because a user could in principle hold both roles —
-- without it the broadcast would double-send.
SELECT DISTINCT ur.user_id
FROM user_roles ur
INNER JOIN roles r ON r.id = ur.role_id
WHERE r.name IN ('admin', 'super_admin');
