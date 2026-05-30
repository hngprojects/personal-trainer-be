package userdevice_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	userdevice "github.com/hngprojects/personal-trainer-be/internal/user_device"
)

func TestServiceRegisterDevice_Success(t *testing.T) {
	userID := uuid.New()
	expected := db.UserDevice{
		ID: uuid.New(), UserID: userID, DeviceToken: "fcm-token-123", Platform: "android", IsActive: true,
	}
	repo := &mockUserDeviceRepo{
		registerDeviceFn: func(_ context.Context, _ uuid.UUID, _, _ string) (db.UserDevice, error) {
			return expected, nil
		},
	}
	svc := userdevice.NewUserDeviceService(repo, testLogger())
	got, err := svc.RegisterDevice(context.Background(), userID, "fcm-token-123", "android")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.ID != expected.ID {
		t.Errorf("expected id %v, got %v", expected.ID, got.ID)
	}
}
func TestServiceRegisterDevice_Error(t *testing.T) {
	repo := &mockUserDeviceRepo{
		registerDeviceFn: func(_ context.Context, _ uuid.UUID, _, _ string) (db.UserDevice, error) {
			return db.UserDevice{}, errors.New("db error")
		},
	}
	svc := userdevice.NewUserDeviceService(repo, testLogger())
	_, err := svc.RegisterDevice(context.Background(), uuid.New(), "token", "ios")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
func TestServiceGetUserDevicesTokens_Success(t *testing.T) {
	userID := uuid.New()
	expected := []db.UserDevice{
		{ID: uuid.New(), UserID: userID, DeviceToken: "token-1", Platform: "android"},
		{ID: uuid.New(), UserID: userID, DeviceToken: "token-2", Platform: "ios"},
	}
	repo := &mockUserDeviceRepo{
		getUserDeviceTokenFn: func(_ context.Context, _ uuid.UUID) ([]db.UserDevice, error) {
			return expected, nil
		},
	}
	svc := userdevice.NewUserDeviceService(repo, testLogger())
	got, err := svc.GetUserDevicesTokens(context.Background(), userID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(got))
	}
}
func TestServiceGetUserDevicesTokens_Empty(t *testing.T) {
	repo := &mockUserDeviceRepo{
		getUserDeviceTokenFn: func(_ context.Context, _ uuid.UUID) ([]db.UserDevice, error) {
			return []db.UserDevice{}, nil
		},
	}
	svc := userdevice.NewUserDeviceService(repo, testLogger())
	got, err := svc.GetUserDevicesTokens(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d items", len(got))
	}
}
func TestServiceGetUserDevicesTokens_Error(t *testing.T) {
	repo := &mockUserDeviceRepo{
		getUserDeviceTokenFn: func(_ context.Context, _ uuid.UUID) ([]db.UserDevice, error) {
			return nil, errors.New("db error")
		},
	}
	svc := userdevice.NewUserDeviceService(repo, testLogger())
	_, err := svc.GetUserDevicesTokens(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
