package notification_test

import (
	"context"
	"io"
	"log/slog"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type mockRepository struct {
	createNotificationFn       func(ctx context.Context, args db.CreateNotificationParams) (db.Notification, error)
	updateNotificationStatusFn func(ctx context.Context, args db.UpdateNotificationStatusParams) error
	getUserNotificationFn      func(ctx context.Context, userID uuid.UUID) (*[]db.Notification, error)
	getUserDeviceTokenFn       func(ctx context.Context, userID uuid.UUID) (*[]db.UserDevice, error)
	getAllActiveUsersDevicesFn func(ctx context.Context) (*[]db.UserDevice, error)
}

func (m *mockRepository) CreateNotification(ctx context.Context, args db.CreateNotificationParams) (db.Notification, error) {
	if m.createNotificationFn != nil {
		return m.createNotificationFn(ctx, args)
	}
	return db.Notification{}, nil
}

func (m *mockRepository) UpdateNotificationStatus(ctx context.Context, args db.UpdateNotificationStatusParams) error {
	if m.updateNotificationStatusFn != nil {
		return m.updateNotificationStatusFn(ctx, args)
	}
	return nil
}

func (m *mockRepository) GetUserNotification(ctx context.Context, userID uuid.UUID) (*[]db.Notification, error) {
	if m.getUserNotificationFn != nil {
		return m.getUserNotificationFn(ctx, userID)
	}
	return &[]db.Notification{}, nil
}

func (m *mockRepository) GetUserDeviceToken(ctx context.Context, userID uuid.UUID) (*[]db.UserDevice, error) {
	if m.getUserDeviceTokenFn != nil {
		return m.getUserDeviceTokenFn(ctx, userID)
	}
	return &[]db.UserDevice{}, nil
}

func (m *mockRepository) GetAllActiveUsersDevices(ctx context.Context) (*[]db.UserDevice, error) {
	if m.getAllActiveUsersDevicesFn != nil {
		return m.getAllActiveUsersDevicesFn(ctx)
	}
	return &[]db.UserDevice{}, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
