package notification_test

import (
	"context"
	"io"
	"log/slog"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type mockRepository struct {
	createNotificationFn         func(ctx context.Context, args db.CreateNotificationParams) (db.Notification, error)
	updateNotificationStatusFn   func(ctx context.Context, args db.UpdateNotificationStatusParams) error
	getUserNotificationFn        func(ctx context.Context, userID uuid.UUID) (*[]db.Notification, error)
	getUserDeviceTokenFn         func(ctx context.Context, userID uuid.UUID) (*[]db.UserDevice, error)
	getAllActiveUsersDevicesFn   func(ctx context.Context) (*[]db.UserDevice, error)
	createNotificationWithTypeFn func(ctx context.Context, args db.CreateNotificationWithTypeParams) (db.Notification, error)
	getUserPendingNotificationFn func(ctx context.Context, userID uuid.UUID) ([]db.Notification, error)
	getUserRoleByUserIDFn        func(ctx context.Context, userID uuid.UUID) (string, error)
	listAdminUserIDsFn           func(ctx context.Context) ([]uuid.UUID, error)
	deactivateDeviceFn           func(ctx context.Context, deviceID uuid.UUID) error
}

func (m *mockRepository) CreateNotification(ctx context.Context, args db.CreateNotificationParams) (db.Notification, error) {
	if m.createNotificationFn != nil {
		return m.createNotificationFn(ctx, args)
	}
	return db.Notification{}, nil
}

func (m *mockRepository) CreateNotificationWithType(ctx context.Context, args db.CreateNotificationWithTypeParams) (db.Notification, error) {
	if m.createNotificationWithTypeFn != nil {
		return m.createNotificationWithTypeFn(ctx, args)
	}
	return db.Notification{}, nil
}

func (m *mockRepository) GetUserPendingNotification(ctx context.Context, userID uuid.UUID) ([]db.Notification, error) {
	if m.getUserPendingNotificationFn != nil {
		return m.getUserPendingNotificationFn(ctx, userID)
	}
	return []db.Notification{}, nil
}

func (m *mockRepository) GetUserRoleByUserID(ctx context.Context, userID uuid.UUID) (string, error) {
	if m.getUserRoleByUserIDFn != nil {
		return m.getUserRoleByUserIDFn(ctx, userID)
	}
	return "", nil
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

func (m *mockRepository) ListAdminUserIDs(ctx context.Context) ([]uuid.UUID, error) {
	if m.listAdminUserIDsFn != nil {
		return m.listAdminUserIDsFn(ctx)
	}
	return nil, nil
}

func (m *mockRepository) DeactivateDevice(ctx context.Context, deviceID uuid.UUID) error {
	if m.deactivateDeviceFn != nil {
		return m.deactivateDeviceFn(ctx, deviceID)
	}
	return nil
}

type mockWSHub struct {
	sendToUserFn         func(userID uuid.UUID, message []byte) bool
	userHasConnectionsFn func(userID uuid.UUID) bool
}

func (m *mockWSHub) SendToUser(userID uuid.UUID, message []byte) bool {
	if m.sendToUserFn != nil {
		return m.sendToUserFn(userID, message)
	}
	return false
}

func (m *mockWSHub) UserHasConnections(userID uuid.UUID) bool {
	if m.userHasConnectionsFn != nil {
		return m.userHasConnectionsFn(userID)
	}
	return false
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
