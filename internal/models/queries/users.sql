-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1 AND deleted_at IS NULL LIMIT 1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1 AND deleted_at IS NULL LIMIT 1;

-- name: CreateUser :one
INSERT INTO users (email, name, password_hash)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateLastLogin :exec
UPDATE users SET last_login = NOW() WHERE id = $1;

-- name: ListUserRoleNames :many
SELECT r.name FROM roles r
JOIN user_roles ur ON ur.role_id = r.id
WHERE ur.user_id = $1;

-- name: AssignRoleToUser :exec
INSERT INTO user_roles (user_id, role_id)
SELECT $1, id FROM roles WHERE name = $2
ON CONFLICT DO NOTHING;
