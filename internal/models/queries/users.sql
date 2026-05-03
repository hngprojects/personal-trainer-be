-- name: CreateUser :one
INSERT INTO users (email, name, auth_provider) VALUES ($1, $2, $3) RETURNING *;


-- name: GetUserByEmailAndProvider :one
SELECT * FROM users WHERE email = $1 AND auth_provider = $2 LIMIT 1;