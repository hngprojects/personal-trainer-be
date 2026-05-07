package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

var ErrNotFound = errors.New("not found")
var ErrEmailExists = errors.New("email already registered")

// UserRepository defines what the auth feature needs from the users table.
type UserRepository interface {
	FindByEmailAndProvider(ctx context.Context, email, provider string) (*db.User, error)
	Create(ctx context.Context, email, name, provider string) (*db.User, error)
	CreateEmailUser(ctx context.Context, email string) (*db.User, error)
	MarkVerified(ctx context.Context, email string) (*db.User, error)
}

// SessionRepository defines what the auth feature needs from the sessions table.
type SessionRepository interface {
	Create(ctx context.Context, userID uuid.UUID, token string, expiresAt time.Time) (*db.Session, error)
}

// VerificationCodeRepository defines what the auth feature needs from the verification_codes table.
type VerificationCodeRepository interface {
	Create(ctx context.Context, email, code string, expiresAt time.Time) error
	GetByEmailAndCode(ctx context.Context, email, code string) (*db.VerificationCode, error)
	DeleteByEmail(ctx context.Context, email string) error
}

// postgresUserRepo implements UserRepository using sqlc-generated queries.
type postgresUserRepo struct {
	q *db.Queries
}

func NewPostgresUserRepo(q *db.Queries) UserRepository {
	return &postgresUserRepo{q: q}
}

func (r *postgresUserRepo) FindByEmailAndProvider(ctx context.Context, email, provider string) (*db.User, error) {
	user, err := r.q.GetUserByEmailAndProvider(ctx, db.GetUserByEmailAndProviderParams{
		Email:        email,
		AuthProvider: provider,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (r *postgresUserRepo) Create(ctx context.Context, email, name, provider string) (*db.User, error) {
	user, err := r.q.CreateUser(ctx, db.CreateUserParams{
		Email:        email,
		Name:         name,
		AuthProvider: provider,
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *postgresUserRepo) CreateEmailUser(ctx context.Context, email string) (*db.User, error) {
	user, err := r.q.CreateEmailUser(ctx, email)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, ErrEmailExists
		}
		return nil, err
	}
	return &user, nil
}

func (r *postgresUserRepo) MarkVerified(ctx context.Context, email string) (*db.User, error) {
	user, err := r.q.MarkUserVerified(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

// postgresSessionRepo implements SessionRepository using sqlc-generated queries.
type postgresSessionRepo struct {
	q *db.Queries
}

func NewPostgresSessionRepo(q *db.Queries) SessionRepository {
	return &postgresSessionRepo{q: q}
}

func (r *postgresSessionRepo) Create(ctx context.Context, userID uuid.UUID, token string, expiresAt time.Time) (*db.Session, error) {
	session, err := r.q.CreateSession(ctx, db.CreateSessionParams{
		UserID:    userID,
		Token:     token,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// postgresVerificationCodeRepo implements VerificationCodeRepository.
type postgresVerificationCodeRepo struct {
	q *db.Queries
}

func NewPostgresVerificationCodeRepo(q *db.Queries) VerificationCodeRepository {
	return &postgresVerificationCodeRepo{q: q}
}

func (r *postgresVerificationCodeRepo) Create(ctx context.Context, email, code string, expiresAt time.Time) error {
	return r.q.CreateVerificationCode(ctx, db.CreateVerificationCodeParams{
		Email:     email,
		Code:      code,
		ExpiresAt: expiresAt,
	})
}

func (r *postgresVerificationCodeRepo) GetByEmailAndCode(ctx context.Context, email, code string) (*db.VerificationCode, error) {
	vc, err := r.q.GetVerificationCode(ctx, email, code)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &vc, nil
}

func (r *postgresVerificationCodeRepo) DeleteByEmail(ctx context.Context, email string) error {
	return r.q.DeleteVerificationCodes(ctx, email)
}
