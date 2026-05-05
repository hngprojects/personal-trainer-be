-- name: CreateUser :one
INSERT INTO users (email, name, auth_provider) VALUES ($1, $2, $3) RETURNING *;

-- name: CreateLocalUser :one
INSERT INTO users (email, name, auth_provider, is_active) VALUES ($1, $2, 'local', false) RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1 LIMIT 1;

-- name: GetUserByEmailAndProvider :one
SELECT * FROM users WHERE email = $1 AND auth_provider = $2 LIMIT 1;

-- name: UpdateUserPassword :exec
UPDATE users SET password = $1, updated_at = NOW() WHERE id = $2;

-- name: ActivateUser :exec
UPDATE users SET name = $1, is_active = true, updated_at = NOW() WHERE id = $2;
