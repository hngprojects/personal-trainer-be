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
	FindByEmail(ctx context.Context, email string) (*db.User, error)
	Create(ctx context.Context, email, name string) (*db.User, error)
	CreateLocal(ctx context.Context, email, name, passwordHash string) (*db.User, error)
	UpdateLastLogin(ctx context.Context, id uuid.UUID) error
	ListRoleNames(ctx context.Context, userID uuid.UUID) ([]string, error)
	AssignRole(ctx context.Context, userID uuid.UUID, roleName string) error
	WithTx(tx *sql.Tx) UserRepository
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

func (r *postgresUserRepo) FindByEmail(ctx context.Context, email string) (*db.User, error) {
	user, err := r.q.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pkgerrors.ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (r *postgresUserRepo) Create(ctx context.Context, email, name string) (*db.User, error) {
	user, err := r.q.CreateUser(ctx, db.CreateUserParams{
		Email:        email,
		Name:         name,
		PasswordHash: sql.NullString{Valid: false},
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *postgresUserRepo) CreateLocal(ctx context.Context, email, name, passwordHash string) (*db.User, error) {
	user, err := r.q.CreateUser(ctx, db.CreateUserParams{
		Email:        email,
		Name:         name,
		PasswordHash: sql.NullString{String: passwordHash, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *postgresUserRepo) UpdateLastLogin(ctx context.Context, id uuid.UUID) error {
	return r.q.UpdateLastLogin(ctx, id)
}

func (r *postgresUserRepo) ListRoleNames(ctx context.Context, userID uuid.UUID) ([]string, error) {
	return r.q.ListUserRoleNames(ctx, userID)
}

func (r *postgresUserRepo) AssignRole(ctx context.Context, userID uuid.UUID, roleName string) error {
	return r.q.AssignRoleToUser(ctx, db.AssignRoleToUserParams{
		UserID: userID,
		Name:   roleName,
	})
}

func (r *postgresUserRepo) WithTx(tx *sql.Tx) UserRepository {
	return &postgresUserRepo{q: r.q.WithTx(tx)}
}
