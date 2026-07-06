package notification

import (
	"context"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type Repository struct {
	q *db.Queries
}

func NewRepository(q *db.Queries) *Repository {
	return &Repository{q: q}
}

type RepositoryInterface interface {
	CreateNotificationWithType(ctx context.Context, args db.CreateNotificationWithTypeParams) (db.Notification, error)
	CreateNotification(ctx context.Context, args db.CreateNotificationParams) (db.Notification, error)
	GetUserPendingNotification(ctx context.Context, userID uuid.UUID) ([]db.Notification, error)
	UpdateNotificationStatus(ctx context.Context, args db.UpdateNotificationStatusParams) error
	GetUserNotification(ctx context.Context, userID uuid.UUID) (*[]db.Notification, error)
	GetUserDeviceToken(ctx context.Context, userID uuid.UUID) (*[]db.UserDevice, error)
	GetUserRoleByUserID(ctx context.Context, userID uuid.UUID) (string, error)
	GetAllActiveUsersDevices(ctx context.Context) (*[]db.UserDevice, error)
	ListAdminUserIDs(ctx context.Context) ([]uuid.UUID, error)
	// DeactivateDevice marks a user_device row inactive so future
	// pushes skip it. Called when FCM returns a permanent failure
	// for the token (uninstalled, rotated, sender-mismatch) — the
	// row is kept for audit but won't be hit on the next push.
	DeactivateDevice(ctx context.Context, deviceID uuid.UUID) error
}

func (r *Repository) CreateNotificationWithType(ctx context.Context, args db.CreateNotificationWithTypeParams) (db.Notification, error) {
	return r.q.CreateNotificationWithType(ctx, args)
}

func (r *Repository) GetUserPendingNotification(ctx context.Context, userID uuid.UUID) ([]db.Notification, error) {
	return r.q.GetPendingRealTimeNotification(ctx, userID)
}

func (r *Repository) GetUserRoleByUserID(ctx context.Context, userID uuid.UUID) (string, error) {
	return r.q.GetUserRoleByID(ctx, userID)
}

func (r *Repository) CreateNotification(ctx context.Context, args db.CreateNotificationParams) (db.Notification, error) {
	return r.q.CreateNotification(ctx, args)
}

func (r *Repository) GetUserDeviceToken(ctx context.Context, userID uuid.UUID) (*[]db.UserDevice, error) {
	// Active-only filter at the query layer: deactivated rows (token
	// rotated, user logged out, FCM previously rejected the token)
	// must not be retried on every push or we waste FCM quota and
	// the SendToUser aggregate "failed" count climbs forever.
	device, err := r.q.ListUserActiveDevicesByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &device, nil
}

func (r *Repository) DeactivateDevice(ctx context.Context, deviceID uuid.UUID) error {
	return r.q.DeactivateUserDevice(ctx, deviceID)
}

func (r *Repository) UpdateNotificationStatus(ctx context.Context, args db.UpdateNotificationStatusParams) error {
	if err := r.q.UpdateNotificationStatus(ctx, args); err != nil {
		return err
	}
	return nil
}

func (r *Repository) GetUserNotification(ctx context.Context, userID uuid.UUID) (*[]db.Notification, error) {
	notifications, err := r.q.GetUserNotification(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &notifications, nil
}

func (r *Repository) GetAllActiveUsersDevices(ctx context.Context) (*[]db.UserDevice, error) {
	devices, err := r.q.GetAllActiveUsersDevices(ctx)
	if err != nil {
		return nil, err
	}
	return &devices, nil
}

func (r *Repository) ListAdminUserIDs(ctx context.Context) ([]uuid.UUID, error) {
	return r.q.ListAdminUserIDs(ctx)
}
