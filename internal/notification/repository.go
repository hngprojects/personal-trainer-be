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
	CreateNotification(ctx context.Context, args db.CreateNotificationParams) (db.Notification, error)
	UpdateNotificationStatus(ctx context.Context, args db.UpdateNotificationStatusParams) error
	GetUserDeviceToken(ctx context.Context, userID uuid.UUID) (*[]db.UserDevice, error)
	GetAllActiveUsersDevices(ctx context.Context) (*[]db.UserDevice, error)
}

func (r *Repository) CreateNotification(ctx context.Context, args db.CreateNotificationParams) (db.Notification, error) {
	return r.q.CreateNotification(ctx, args)
}

func (r *Repository) GetUserDeviceToken(ctx context.Context, userID uuid.UUID) (*[]db.UserDevice, error) {
	device, err := r.q.GetUserDevicesByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &device, nil
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
