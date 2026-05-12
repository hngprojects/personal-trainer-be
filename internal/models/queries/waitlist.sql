-- name: AddWaitlist :execresult
INSERT INTO waitlist (email, phone_number, location, name)
VALUES ($1, $2, $3, $4)
ON CONFLICT (email) DO NOTHING;

-- name: GetWaitlist :many
SELECT * FROM waitlist;

-- name: GetSingleWaitlist :one
SELECT * FROM waitlist
WHERE email = $1;