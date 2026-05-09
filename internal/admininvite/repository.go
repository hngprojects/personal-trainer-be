package admininvite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	pkgerrors "github.com/hngprojects/personal-trainer-be/pkg/errors"
)

// Repository defines what the admin-invite feature needs from the admin_invites table.
type Repository interface {
	Create(ctx context.Context, email, name, tokenHash string, invitedBy uuid.UUID, expiresAt time.Time) (*db.AdminInvite, error)
	FindByHash(ctx context.Context, tokenHash string) (*db.AdminInvite, error)
	MarkAccepted(ctx context.Context, id uuid.UUID) error
	HasPendingForEmail(ctx context.Context, email string) (bool, error)
	WithTx(tx *sql.Tx) Repository
}

// postgresRepo implements Repository using sqlc-generated queries.
type postgresRepo struct {
	q *db.Queries
}

func NewPostgresRepo(q *db.Queries) Repository {
	return &postgresRepo{q: q}
}

func (r *postgresRepo) Create(ctx context.Context, email, name, tokenHash string, invitedBy uuid.UUID, expiresAt time.Time) (*db.AdminInvite, error) {
	inv, err := r.q.CreateAdminInvite(ctx, db.CreateAdminInviteParams{
		Email:     email,
		Name:      name,
		TokenHash: tokenHash,
		InvitedBy: invitedBy,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

func (r *postgresRepo) FindByHash(ctx context.Context, tokenHash string) (*db.AdminInvite, error) {
	inv, err := r.q.GetAdminInviteByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pkgerrors.ErrNotFound
		}
		return nil, err
	}
	return &inv, nil
}

func (r *postgresRepo) MarkAccepted(ctx context.Context, id uuid.UUID) error {
	return r.q.MarkAdminInviteAccepted(ctx, id)
}

func (r *postgresRepo) HasPendingForEmail(ctx context.Context, email string) (bool, error) {
	return r.q.HasPendingInviteForEmail(ctx, email)
}

func (r *postgresRepo) WithTx(tx *sql.Tx) Repository {
	return &postgresRepo{q: r.q.WithTx(tx)}
}
