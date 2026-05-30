-- name: UpsertPasswordResetCode :exec
-- Atomically replaces any prior code for the email. Combined with the
-- UNIQUE(email) constraint on password_reset_codes, this guarantees at most
-- one valid code per email at any time.
INSERT INTO password_reset_codes (email, code, expires_at)
VALUES ($1, $2, $3)
ON CONFLICT (email) DO UPDATE
    SET code       = EXCLUDED.code,
        expires_at = EXCLUDED.expires_at,
        created_at = NOW();

-- name: ConsumePasswordResetCode :one
DELETE FROM password_reset_codes
WHERE email = $1 AND code = $2 AND expires_at > NOW()
RETURNING *;

-- name: DeletePasswordResetCodes :exec
DELETE FROM password_reset_codes WHERE email = $1;

-- name: UpdateUserPassword :one
-- The is_active = true filter is defense-in-depth: even if the handler's
-- pre-check passes, this statement returns no rows for an inactive account
-- (e.g. if the account is deactivated mid-flow), and the caller surfaces a
-- generic invalid-code error.
UPDATE users SET password = $2, updated_at = NOW()
WHERE email = $1 AND auth_provider = 'local' AND is_active = true
RETURNING *;

-- name: DeleteSessionsByUserID :exec
DELETE FROM sessions WHERE user_id = $1;

-- name: VerifyPasswordResetCode :one
-- Read-only check: confirms the code is valid and not expired without consuming it.
-- Used by the verify-otp step so mobile can confirm the code before showing the new-password screen.
SELECT * FROM password_reset_codes
WHERE email = $1 AND code = $2 AND expires_at > NOW();
