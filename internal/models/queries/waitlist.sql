-- name: AddWaitlist :execresult
INSERT INTO waitlist (email, feedback)
VALUES ($1, $2)
ON CONFLICT (email) DO NOTHING;

-- name: GetWaitlist :many
SELECT * FROM waitlist;