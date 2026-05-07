package db

import (
	"context"
	"time"
)

const createEmailUser = `
INSERT INTO users (email, auth_provider, is_active)
VALUES ($1, 'local', false)
RETURNING id, email, COALESCE(name, ''), COALESCE(password, ''), auth_provider, is_active, created_at, updated_at
`

func (q *Queries) CreateEmailUser(ctx context.Context, email string) (User, error) {
	row := q.db.QueryRowContext(ctx, createEmailUser, email)
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

const markUserVerified = `
UPDATE users SET is_active = true, updated_at = NOW()
WHERE email = $1 AND auth_provider = 'local'
RETURNING id, email, COALESCE(name, ''), COALESCE(password, ''), auth_provider, is_active, created_at, updated_at
`

func (q *Queries) MarkUserVerified(ctx context.Context, email string) (User, error) {
	row := q.db.QueryRowContext(ctx, markUserVerified, email)
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

const createVerificationCode = `
INSERT INTO verification_codes (email, code, expires_at)
VALUES ($1, $2, $3)
`

type CreateVerificationCodeParams struct {
	Email     string
	Code      string
	ExpiresAt time.Time
}

func (q *Queries) CreateVerificationCode(ctx context.Context, arg CreateVerificationCodeParams) error {
	_, err := q.db.ExecContext(ctx, createVerificationCode, arg.Email, arg.Code, arg.ExpiresAt)
	return err
}

const getVerificationCode = `
SELECT id, email, code, created_at, expires_at
FROM verification_codes
WHERE email = $1 AND code = $2 AND expires_at > NOW()
LIMIT 1
`

func (q *Queries) GetVerificationCode(ctx context.Context, email, code string) (VerificationCode, error) {
	row := q.db.QueryRowContext(ctx, getVerificationCode, email, code)
	var v VerificationCode
	err := row.Scan(&v.ID, &v.Email, &v.Code, &v.CreatedAt, &v.ExpiresAt)
	return v, err
}

const deleteVerificationCodes = `DELETE FROM verification_codes WHERE email = $1`

func (q *Queries) DeleteVerificationCodes(ctx context.Context, email string) error {
	_, err := q.db.ExecContext(ctx, deleteVerificationCodes, email)
	return err
}
