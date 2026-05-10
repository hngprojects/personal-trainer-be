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
