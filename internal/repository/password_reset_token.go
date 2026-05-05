package repository

import (
	"context"
	"database/sql"

	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/internal/models"
)

type PasswordResetRepository struct {
	db *sql.DB
	q  *db.Queries
}

func NewPasswordResetRepository(database *sql.DB, q *db.Queries) *PasswordResetRepository {
	return &PasswordResetRepository{db: database, q: q}
}

func (r *PasswordResetRepository) Save(ctx context.Context, prt *models.PasswordResetToken) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	qtx := r.q.WithTx(tx)
	if err := qtx.DeletePasswordResetTokensByUserID(ctx, prt.UserID); err != nil {
		return err
	}

	row, err := qtx.CreatePasswordResetToken(ctx, db.CreatePasswordResetTokenParams{
		UserID:    prt.UserID,
		TokenHash: prt.TokenHash,
		ExpiresAt: prt.ExpiresAt,
	})
	if err != nil {
		return err
	}

	prt.ID = row.ID
	prt.CreatedAt = row.CreatedAt
	return tx.Commit()
}
