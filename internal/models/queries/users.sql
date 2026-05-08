-- name: CreateUser :one
INSERT INTO users (email, name, auth_provider)
VALUES ($1, $2, $3)
ON CONFLICT (email, auth_provider) DO UPDATE
    SET updated_at = NOW()
RETURNING *;

-- name: GetUserByEmailAndProvider :one
SELECT * 
    FROM users 
    WHERE email = $1 AND auth_provider = $2 LIMIT 1;

<<<<<<< HEAD
-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1 LIMIT 1;
=======
-- name: GetUserRoleByID :one
<<<<<<< HEAD
SELECT role FROM users WHERE id = $1 LIMIT 1;
>>>>>>> c0e53e7 (Completed Trainers Features)
=======
SELECT role
    FROM users 
    WHERE id = $1 
    LIMIT 1;
<<<<<<< HEAD
>>>>>>> ab21b8d (Refactor SQL queries for better formatting)
=======

>>>>>>> 9fc594e (Add newline at the end of users.sql)
