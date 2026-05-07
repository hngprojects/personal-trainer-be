-- name: AddWaitlist :execresult
INSERT INTO waitlist (email, feedback)
VALUES ($1, $2);

-- name: GetWaitlist :many
SELECT * FROM waitlist;