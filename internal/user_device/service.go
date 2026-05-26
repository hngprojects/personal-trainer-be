package userdevice

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type userDeviceService struct {
	repo *userDeviceRepository
	log  *slog.Logger
}

type UserDeviceServiceInterface interface {
	RegisterDevice(ctx context.Context, userID uuid.UUID, deviceToken string, platform string) (*db.UserDevice, error)
	GetUserDevicesTokens(ctx context.Context, userID uuid.UUID) ([]db.UserDevice, error)
}

func NewUserDeviceService(repo *userDeviceRepository, log *slog.Logger) *userDeviceService {
	return &userDeviceService{
		repo: repo,
		log:  log,
	}
}

func (s *userDeviceService) RegisterDevice(ctx context.Context, userID uuid.UUID, deviceToken string, platform string) (*db.UserDevice, error) {
	userDevice, err := s.repo.RegisterDevice(ctx, userID, deviceToken, platform)
	if err != nil {
		return nil, err
	}
	return &userDevice, nil
}

func (s *userDeviceService) GetUserDevicesTokens(ctx context.Context, userID uuid.UUID) ([]db.UserDevice, error) {
	userDevices, err := s.repo.GetUserDeviceToken(ctx, userID)
	if err != nil {
		return nil, err
	}
	return userDevices, nil
}
