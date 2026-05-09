package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	pkgerrors "github.com/hngprojects/personal-trainer-be/pkg/errors"
)

// UserRepository defines what the auth feature needs from the users table.
type UserRepository interface {
	FindByEmailAndProvider(ctx context.Context, email, provider string) (*db.User, error)
	Create(ctx context.Context, email, name, provider string) (*db.User, error)
}

// SessionRepository defines what the auth feature needs from the sessions table.
type SessionRepository interface {
	Create(ctx context.Context, userID uuid.UUID, token string, expiresAt time.Time) (*db.Session, error)
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
			return nil, pkgerrors.ErrNotFound
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
