package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

type PasswordResetCode struct {
	ID        uuid.UUID
	Email     string
	Code      string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// upsertPasswordResetCode atomically replaces any prior reset code for an
// email. Relies on UNIQUE(email) in the table — the conflict target.
const upsertPasswordResetCode = `
INSERT INTO password_reset_codes (email, code, expires_at)
VALUES ($1, $2, $3)
ON CONFLICT (email) DO UPDATE
    SET code       = EXCLUDED.code,
        expires_at = EXCLUDED.expires_at,
        created_at = NOW()
`

type UpsertPasswordResetCodeParams struct {
	Email     string
	Code      string
	ExpiresAt time.Time
}

func (q *Queries) UpsertPasswordResetCode(ctx context.Context, arg UpsertPasswordResetCodeParams) error {
	_, err := q.db.ExecContext(ctx, upsertPasswordResetCode, arg.Email, arg.Code, arg.ExpiresAt)
	return err
}

const consumePasswordResetCode = `
DELETE FROM password_reset_codes
WHERE email = $1 AND code = $2 AND expires_at > NOW()
RETURNING id, email, code, created_at, expires_at
`

func (q *Queries) ConsumePasswordResetCode(ctx context.Context, email, code string) (PasswordResetCode, error) {
	row := q.db.QueryRowContext(ctx, consumePasswordResetCode, email, code)
	var v PasswordResetCode
	err := row.Scan(&v.ID, &v.Email, &v.Code, &v.CreatedAt, &v.ExpiresAt)
	return v, err
}

const deletePasswordResetCodes = `DELETE FROM password_reset_codes WHERE email = $1`

func (q *Queries) DeletePasswordResetCodes(ctx context.Context, email string) error {
	_, err := q.db.ExecContext(ctx, deletePasswordResetCodes, email)
	return err
}

// ErrEmptyPassword is returned by UpdateUserPassword if it is called with an
// empty password string. Storing an empty / NULL password would let an
// attacker authenticate by submitting an empty password against bcrypt, since
// bcrypt.CompareHashAndPassword on a NULL/empty hash short-circuits.
var ErrEmptyPassword = errors.New("password must not be empty")

// updateUserPassword updates an admin's password atomically. The EXISTS
// subquery re-checks the admin role inside the same statement (and inside the
// surrounding transaction), so a role revoked between the handler-level role
// check and this UPDATE will cause the row to be filtered out — the caller
// then sees sql.ErrNoRows and surfaces the same generic "invalid or expired
// reset code" response.
const updateUserPassword = `
UPDATE users SET password = $2, updated_at = NOW()
WHERE email = $1
  AND auth_provider = 'local'
  AND is_active = true
  AND EXISTS (
      SELECT 1 FROM user_roles ur
      INNER JOIN roles r ON r.id = ur.role_id
      WHERE ur.user_id = users.id AND r.name = 'admin'
  )
RETURNING id, email, COALESCE(name, ''), password, auth_provider, is_active, created_at, updated_at
`

// UpdateUserPassword takes a plain (already-hashed) password string rather
// than sql.NullString so the API physically cannot store NULL — an auth-bypass
// vector that the previous signature left exposed.
func (q *Queries) UpdateUserPassword(ctx context.Context, email, password string) (User, error) {
	if password == "" {
		return User{}, ErrEmptyPassword
	}
	row := q.db.QueryRowContext(ctx, updateUserPassword, email, sql.NullString{String: password, Valid: true})
	var i User
	err := row.Scan(
		&i.ID,
		&i.Email,
		&i.Name,
		&i.Password,
		&i.AuthProvider,
		&i.IsActive,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const deleteSessionsByUserID = `DELETE FROM sessions WHERE user_id = $1`

func (q *Queries) DeleteSessionsByUserID(ctx context.Context, userID uuid.UUID) error {
	_, err := q.db.ExecContext(ctx, deleteSessionsByUserID, userID)
	return err
}
