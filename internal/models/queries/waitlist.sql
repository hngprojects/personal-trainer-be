-- name: AddWaitlist :execresult
INSERT INTO waitlist (email)
VALUES ($1)
ON CONFLICT (email) DO NOTHING;

-- name: GetWaitlist :many
SELECT * FROM waitlist;

-- name: GetSingleWaitlist :one
SELECT * FROM waitlist
WHERE email = $1;