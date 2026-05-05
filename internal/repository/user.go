package repository

import (
	"context"
	"database/sql"

	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/internal/models"
)

type UserRepository struct {
	q *db.Queries
}

func NewUserRepository(q *db.Queries) *UserRepository {
	return &UserRepository{q: q}
}

func (r *UserRepository) Create(ctx context.Context, user *models.User) error {
	row, err := r.q.CreateLocalUser(ctx, db.CreateLocalUserParams{
		Email: user.Email,
		Name:  "",
	})
	if err != nil {
		return err
	}
	user.ID = row.ID
	user.CreatedAt = row.CreatedAt
	user.UpdatedAt = row.UpdatedAt
	return nil
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*models.User, error) {
	row, err := r.q.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	return toUserModel(row), nil
}

func (r *UserRepository) UpdatePassword(ctx context.Context, userID string, hashedPassword string) error {
	return r.q.UpdateUserPassword(ctx, db.UpdateUserPasswordParams{
		ID:       userID,
		Password: sql.NullString{String: hashedPassword, Valid: true},
	})
}

func (r *UserRepository) UpdateNameAndActivate(ctx context.Context, userID string, name string) error {
	return r.q.ActivateUser(ctx, db.ActivateUserParams{
		ID:   userID,
		Name: name,
	})
}

func toUserModel(row db.User) *models.User {
	var password *string
	if row.Password.Valid {
		password = &row.Password.String
	}
	return &models.User{
		ID:           row.ID,
		Email:        row.Email,
		Name:         row.Name,
		Password:     password,
		AuthProvider: models.AuthProvider(row.AuthProvider),
		IsActive:     row.IsActive,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}
