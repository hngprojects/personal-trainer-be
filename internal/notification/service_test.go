package notification_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/notification"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	fcmnotif "github.com/hngprojects/personal-trainer-be/pkg/notification"
)

func disabledFCM() *fcmnotif.PushNotification {
	return fcmnotif.NewPushNotification("", "", nil, testLogger())
}

func TestServiceSendNotificationToUser_Success(t *testing.T) {
	userID := uuid.New()
	now := time.Now()
	repo := &mockRepository{
		createNotificationFn: func(_ context.Context, args db.CreateNotificationParams) (db.Notification, error) {
			return db.Notification{
				ID: uuid.New(), UserID: args.UserID, Title: args.Title,
				Message: args.Message, Status: "pending", CreatedAt: now, UpdatedAt: now,
			}, nil
		},
		getUserDeviceTokenFn: func(_ context.Context, _ uuid.UUID) (*[]db.UserDevice, error) {
			return &[]db.UserDevice{}, nil
		},
	}
	svc := notification.NewNotificationService(repo, disabledFCM(), testLogger())

	resp, err := svc.SendNotificationToUser(context.Background(), userID, "Title", "Message", "idem-123")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Status != "pending" {
		t.Errorf("expected status 'pending', got %q", resp.Status)
	}
	if resp.Title != "Title" {
		t.Errorf("expected title 'Title', got %q", resp.Title)
	}
}

func TestServiceSendNotificationToUser_CreateError(t *testing.T) {
	repo := &mockRepository{
		createNotificationFn: func(_ context.Context, _ db.CreateNotificationParams) (db.Notification, error) {
			return db.Notification{}, errors.New("db error")
		},
	}
	svc := notification.NewNotificationService(repo, disabledFCM(), testLogger())

	_, err := svc.SendNotificationToUser(context.Background(), uuid.New(), "Title", "Msg", "key")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestServiceSendNotificationToUser_NoDevices(t *testing.T) {
	repo := &mockRepository{
		createNotificationFn: func(_ context.Context, args db.CreateNotificationParams) (db.Notification, error) {
			return db.Notification{
				ID: uuid.New(), UserID: args.UserID, Title: args.Title,
				Message: args.Message, Status: "pending",
			}, nil
		},
		getUserDeviceTokenFn: func(_ context.Context, _ uuid.UUID) (*[]db.UserDevice, error) {
			return &[]db.UserDevice{}, nil
		},
	}
	svc := notification.NewNotificationService(repo, disabledFCM(), testLogger())

	resp, err := svc.SendNotificationToUser(context.Background(), uuid.New(), "Title", "Msg", "key")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
}

func TestServiceSendNotificationToUser_DeviceTokenError(t *testing.T) {
	userID := uuid.New()
	now := time.Now()
	repo := &mockRepository{
		createNotificationFn: func(_ context.Context, args db.CreateNotificationParams) (db.Notification, error) {
			return db.Notification{
				ID: uuid.New(), UserID: args.UserID, Title: args.Title,
				Message: args.Message, Status: "pending", CreatedAt: now, UpdatedAt: now,
			}, nil
		},
		getUserDeviceTokenFn: func(_ context.Context, _ uuid.UUID) (*[]db.UserDevice, error) {
			return nil, errors.New("token fetch error")
		},
	}
	svc := notification.NewNotificationService(repo, disabledFCM(), testLogger())

	resp, err := svc.SendNotificationToUser(context.Background(), userID, "Title", "Msg", "key")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if resp == nil {
		t.Fatal("expected response even on error, got nil")
	}
}

func TestServiceSendNotificationToUser_FCMDisabled(t *testing.T) {
	userID := uuid.New()
	now := time.Now()
	var statusUpdated bool
	repo := &mockRepository{
		createNotificationFn: func(_ context.Context, args db.CreateNotificationParams) (db.Notification, error) {
			return db.Notification{
				ID: uuid.New(), UserID: args.UserID, Title: args.Title,
				Message: args.Message, Status: "pending", CreatedAt: now, UpdatedAt: now,
			}, nil
		},
		getUserDeviceTokenFn: func(_ context.Context, _ uuid.UUID) (*[]db.UserDevice, error) {
			return &[]db.UserDevice{
				{DeviceToken: "token-1", IsPushNotificationEnabled: true},
			}, nil
		},
		updateNotificationStatusFn: func(_ context.Context, args db.UpdateNotificationStatusParams) error {
			if args.Status != "failed" {
				t.Errorf("expected status 'failed', got %q", args.Status)
			}
			statusUpdated = true
			return nil
		},
	}
	svc := notification.NewNotificationService(repo, disabledFCM(), testLogger())

	_, err := svc.SendNotificationToUser(context.Background(), userID, "Title", "Msg", "key")
	if err == nil {
		t.Fatal("expected error from disabled FCM, got nil")
	}
	if !statusUpdated {
		t.Error("expected notification status to be updated to 'failed'")
	}
}

func TestServiceGetUserNotification_Success(t *testing.T) {
	userID := uuid.New()
	now := time.Now()
	repo := &mockRepository{
		getUserNotificationFn: func(_ context.Context, _ uuid.UUID) (*[]db.Notification, error) {
			return &[]db.Notification{
				{ID: uuid.New(), UserID: userID, Title: "Notif 1", Message: "Msg 1", Status: "sent", CreatedAt: now, UpdatedAt: now},
				{ID: uuid.New(), UserID: userID, Title: "Notif 2", Message: "Msg 2", Status: "pending", CreatedAt: now, UpdatedAt: now},
			}, nil
		},
	}
	svc := notification.NewNotificationService(repo, disabledFCM(), testLogger())

	resp, err := svc.GetUserNotification(context.Background(), userID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(*resp) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(*resp))
	}
	if (*resp)[0].Title != "Notif 1" {
		t.Errorf("expected title 'Notif 1', got %q", (*resp)[0].Title)
	}
}
