package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"unicode"

	db "github.com/hngprojects/personal-trainer-be/internal/db"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	queries *db.Queries
}

func NewAuthService(queries *db.Queries) *AuthService {
	return &AuthService{queries: queries}
}

func (s *AuthService) Logout(ctx context.Context, token string) error {
	return s.queries.DeleteSessionByToken(ctx, token)
}

func (s *AuthService) ChangePassword(ctx context.Context, userID int64, newPassword string) error {
	if err := validatePassword(newPassword); err != nil {
		return err
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), 12)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	return s.queries.UpdateUserPassword(ctx, db.UpdateUserPasswordParams{
		Password: sql.NullString{String: string(hashed), Valid: true},
		ID:       userID,
	})
}

func validatePassword(p string) error {
	if len(p) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	hasNumber := false
	for _, c := range p {
		if unicode.IsNumber(c) {
			hasNumber = true
			break
		}
	}
	if !hasNumber {
		return errors.New("password must contain at least one number")
	}
	return nil
}