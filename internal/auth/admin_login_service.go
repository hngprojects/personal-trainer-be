package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"golang.org/x/crypto/bcrypt"
)

type AdminAuthService interface {
	Login(ctx context.Context, email string, password string) (*api.SuccessResponse, error)
}

type adminLoginService struct {
	user UserRepository
	log  *slog.Logger
}

func NewAdminLoginService(user UserRepository, log *slog.Logger) AdminAuthService {
	return &adminLoginService{user: user, log: log}
}

func (r *adminLoginService) Login(ctx context.Context, email string, password string) (*api.SuccessResponse, error) {
	user, err := r.user.FindByEmail(ctx, email)
	if err != nil {
		r.log.Error("error finding provided email", "err", err)
		return nil, errors.New("invalid email or password")
	}
	userRole, err := r.user.GetUserRole(ctx, email)
	if err != nil || userRole.RoleName != "admin" {
		r.log.Error("error getting user role", "err", err)
		return nil, errors.New("invalid email or password")
	}
	if !user.Password.Valid {
		r.log.Error("user does not have password set", "err", err)
		return nil, errors.New("invalid email or password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password.String), []byte(password)); err != nil {
		r.log.Error("password does not match raw password", "err", err)
		return nil, errors.New("invalid email or password")
	}
	accessToken, err := GenerateJWTToken(user.ID.String(), AccessToken)
	if err != nil {
		r.log.Error("error generating access token", "err", err)
		return nil, err
	}
	refreshToken, err := GenerateJWTToken(user.ID.String(), RefreshToken)
	if err != nil {
		r.log.Error("error generating refresh token", "err", err)
		return nil, err
	}
	var successResponse api.SuccessResponse
	tokenData := map[string]interface{}{
		"user": map[string]interface{}{
			"id":               user.ID.String(),
			"email":            user.Email,
			"name":             user.Name,
			"user_type":        userRole.RoleName,
			"profile_complete": true,
		},
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"expires_in":    int(AccessTokenTTL / time.Second),
	}
	successResponse = api.NewSuccessResponse("admin user logged in successfully", api.CodeOK, tokenData, nil)
	return &successResponse, nil
}
