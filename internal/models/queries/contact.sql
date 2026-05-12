-- name: CreateMessage :one
INSERT INTO contact_messages (email, subject, message, name)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetMessageByEmail :many
SELECT *
FROM contact_messages
WHERE email = $1
ORDER BY created_at DESC;

-- name: GetMessageByID :one
SELECT *
FROM contact_messages
WHERE id = $1;