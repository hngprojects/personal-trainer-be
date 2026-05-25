package userdevice

import (
	"context"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type userDeviceRepository struct {
	q *db.Queries
}

func NewUserDeviceRepository(q *db.Queries) *userDeviceRepository {
	return &userDeviceRepository{
		q: q,
	}
}

type UserDeviceInterface interface {
	RegisterDevice(ctx context.Context, userID uuid.UUID, deviceToken string, platform string) (db.UserDevice, error)
	GetUserDeviceToken(ctx context.Context, userID uuid.UUID) ([]db.UserDevice, error)
}

func (r *userDeviceRepository) RegisterDevice(ctx context.Context, userID uuid.UUID, deviceToken string, platform string) (db.UserDevice, error) {
	args := db.CreateUserDeviceParams{
		UserID:      userID,
		DeviceToken: deviceToken,
		Platform:    platform,
	}
	return r.q.CreateUserDevice(ctx, args)
}

func (r *userDeviceRepository) GetUserDeviceToken(ctx context.Context, userID uuid.UUID) ([]db.UserDevice, error) {
	device, err := r.q.GetUserDevicesByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return device, nil
}
