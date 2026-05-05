package repository

import (
	"context"
	"time"

	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/internal/models"
)

type PasswordResetRepository struct {
	q *db.Queries
}

func NewPasswordResetRepository(q *db.Queries) *PasswordResetRepository {
	return &PasswordResetRepository{q: q}
}

func (r *PasswordResetRepository) Save(ctx context.Context, prt *models.PasswordResetToken) error {
	// Invalidate any existing tokens for this user before creating a new one
	_ = r.q.DeletePasswordResetTokensByUserID(ctx, prt.UserID)

	row, err := r.q.CreatePasswordResetToken(ctx, db.CreatePasswordResetTokenParams{
		UserID:    prt.UserID,
		Token:     prt.Token,
		ExpiresAt: prt.ExpiresAt,
	})
	if err != nil {
		return err
	}
	prt.ID = row.ID
	prt.CreatedAt = row.CreatedAt
	return nil
}

func (r *PasswordResetRepository) FindByToken(ctx context.Context, token string) (*models.PasswordResetToken, error) {
	row, err := r.q.GetPasswordResetToken(ctx, token)
	if err != nil {
		return nil, err
	}
	var usedAt *time.Time
	if row.UsedAt.Valid {
		usedAt = &row.UsedAt.Time
	}
	return &models.PasswordResetToken{
		ID:        row.ID,
		UserID:    row.UserID,
		Token:     row.Token,
		ExpiresAt: row.ExpiresAt,
		UsedAt:    usedAt,
		CreatedAt: row.CreatedAt,
	}, nil
}

func (r *PasswordResetRepository) MarkUsed(ctx context.Context, token string) error {
	return r.q.MarkPasswordResetTokenUsed(ctx, token)
}
