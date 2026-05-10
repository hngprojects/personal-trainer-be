-- name: CreateUser :one
INSERT INTO users (email, name, auth_provider)
VALUES ($1, $2, $3)
ON CONFLICT (email, auth_provider) DO UPDATE
    SET updated_at = NOW()
RETURNING *;

-- name: GetUserByEmailAndProvider :one
SELECT *
FROM users
WHERE email = $1 AND auth_provider = $2
LIMIT 1;

-- name: GetUserByID :one
SELECT *
FROM users
WHERE id = $1
LIMIT 1;

-- name: GetUserRoleByID :one
SELECT role
FROM users
WHERE id = $1
LIMIT 1;

-- name: GetUserByEmail :one
SELECT *
FROM users
WHERE email = $1
LIMIT 1;
SELECT * FROM users WHERE id = $1 LIMIT 1;

-- name: GetUserRole :one
SELECT users.id, users.email,
    user_roles.id,
    roles.name
FROM users
JOIN user_roles ON user_roles.user_id=users.id
JOIN roles ON roles.id=user_roles.role_id
WHERE email=$1 LIMIT 1;
