package repository

import (
	"context"

	"github.com/hngprojects/personal-trainer-be/internal/db"
	"github.com/hngprojects/personal-trainer-be/internal/models"
)

type VerificationCodeRepository struct {
	q *db.Queries
}

func NewVerificationCodeRepository(q *db.Queries) *VerificationCodeRepository {
	return &VerificationCodeRepository{q: q}
}

func (r *VerificationCodeRepository) Save(ctx context.Context, vc *models.VerificationCode) error {
	if err := r.q.DeleteVerificationCodesByEmail(ctx, vc.Email); err != nil {
		return err
	}
	row, err := r.q.CreateVerificationCode(ctx, db.CreateVerificationCodeParams{
		Email:     vc.Email,
		Code:      vc.Code,
		ExpiresAt: vc.ExpiresAt,
	})
	if err != nil {
		return err
	}
	vc.ID = row.ID
	vc.CreatedAt = row.CreatedAt
	return nil
}

func (r *VerificationCodeRepository) FindByEmailAndCode(ctx context.Context, email, code string) (*models.VerificationCode, error) {
	row, err := r.q.GetVerificationCode(ctx, db.GetVerificationCodeParams{
		Email: email,
		Code:  code,
	})
	if err != nil {
		return nil, err
	}
	return &models.VerificationCode{
		ID:        row.ID,
		Email:     row.Email,
		Code:      row.Code,
		CreatedAt: row.CreatedAt,
		ExpiresAt: row.ExpiresAt,
	}, nil
}

func (r *VerificationCodeRepository) Delete(ctx context.Context, email string) error {
	return r.q.DeleteVerificationCodesByEmail(ctx, email)
}
