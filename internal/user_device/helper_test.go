package userdevice_test

import (
	"context"
	"io"
	"log/slog"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type mockUserDeviceRepo struct {
	registerDeviceFn     func(ctx context.Context, userID uuid.UUID, deviceToken string, platform string) (db.UserDevice, error)
	getUserDeviceTokenFn func(ctx context.Context, userID uuid.UUID) ([]db.UserDevice, error)
}

func (m *mockUserDeviceRepo) RegisterDevice(ctx context.Context, userID uuid.UUID, deviceToken string, platform string) (db.UserDevice, error) {
	if m.registerDeviceFn != nil {
		return m.registerDeviceFn(ctx, userID, deviceToken, platform)
	}
	return db.UserDevice{}, nil
}
func (m *mockUserDeviceRepo) GetUserDeviceToken(ctx context.Context, userID uuid.UUID) ([]db.UserDevice, error) {
	if m.getUserDeviceTokenFn != nil {
		return m.getUserDeviceTokenFn(ctx, userID)
	}
	return nil, nil
}
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
