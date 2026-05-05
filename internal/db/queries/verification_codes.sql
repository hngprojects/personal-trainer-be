-- name: CreateVerificationCode :one
INSERT INTO verification_codes (email, code, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetVerificationCode :one
SELECT *
FROM verification_codes
WHERE
    email = $1
    AND code = $2
LIMIT 1;

-- name: DeleteVerificationCodesByEmail :exec
DELETE FROM verification_codes
WHERE email = $1;
