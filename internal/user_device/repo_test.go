package userdevice_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type testUserDeviceRepo struct {
	registerDeviceFn     func(ctx context.Context, userID uuid.UUID, deviceToken string, platform string) (db.UserDevice, error)
	getUserDeviceTokenFn func(ctx context.Context, userID uuid.UUID) ([]db.UserDevice, error)
}

func (t *testUserDeviceRepo) RegisterDevice(ctx context.Context, userID uuid.UUID, deviceToken string, platform string) (db.UserDevice, error) {
	if t.registerDeviceFn != nil {
		return t.registerDeviceFn(ctx, userID, deviceToken, platform)
	}
	return db.UserDevice{}, nil
}

func (t *testUserDeviceRepo) GetUserDeviceToken(ctx context.Context, userID uuid.UUID) ([]db.UserDevice, error) {
	if t.getUserDeviceTokenFn != nil {
		return t.getUserDeviceTokenFn(ctx, userID)
	}
	return nil, nil
}

func TestRegisterDevice_Success(t *testing.T) {
	userID := uuid.New()
	expectedResponse := db.UserDevice{
		ID:                        uuid.New(),
		UserID:                    userID,
		DeviceToken:               "fcm-test-token",
		IsPushNotificationEnabled: true,
		Platform:                  "andriod",
		IsActive:                  true,
	}
	repo := &testUserDeviceRepo{
		registerDeviceFn: func(ctx context.Context, userID uuid.UUID, deviceToken, platform string) (db.UserDevice, error) {
			return expectedResponse, nil
		},
	}

	gotResponse, err := repo.RegisterDevice(context.Background(), userID, "fcm-test-token", "andriod")
	if err != nil {
		t.Errorf("exptected no error, got %v", err)
	}
	if gotResponse.ID != expectedResponse.ID {
		t.Errorf("expected id %v, got %v", expectedResponse.ID, gotResponse.ID)
	}
	if gotResponse.DeviceToken != expectedResponse.DeviceToken {
		t.Errorf("expected device token %v, got %v", expectedResponse.DeviceToken, gotResponse.DeviceToken)
	}
	if gotResponse.Platform != expectedResponse.Platform {
		t.Errorf("expected platformn %v, got %v", expectedResponse.Platform, gotResponse.Platform)
	}
}

func TestRegisterDevice_Error(t *testing.T) {
	repo := &testUserDeviceRepo{
		registerDeviceFn: func(ctx context.Context, userID uuid.UUID, deviceToken, platform string) (db.UserDevice, error) {
			return db.UserDevice{}, errors.New("db error")
		},
	}
	_, err := repo.RegisterDevice(context.Background(), uuid.New(), "fcm-test-token", "andriod")
	if err == nil {
		t.Errorf("expected error, got nil")
	}
}

func TestGetUserDeviceToken_Success(t *testing.T) {
	userID := uuid.New()
	expectedResponse := []db.UserDevice{
		{
			ID: uuid.New(), UserID: userID, DeviceToken: "fcm-test-token", IsPushNotificationEnabled: true, IsActive: true,
		},
		{
			ID: uuid.New(), UserID: userID, DeviceToken: "fcm-test-token-2", IsPushNotificationEnabled: true,
		},
		{
			ID: uuid.New(), UserID: userID, DeviceToken: "fcm-test-token-3", IsPushNotificationEnabled: true,
		},
	}
	repo := &testUserDeviceRepo{
		getUserDeviceTokenFn: func(ctx context.Context, userID uuid.UUID) ([]db.UserDevice, error) {
			return expectedResponse, nil
		},
	}
	gotResponse, err := repo.GetUserDeviceToken(context.Background(), userID)
	if err != nil {
		t.Errorf("expected %v, got error %v", expectedResponse, err)
	}
	if len(gotResponse) != 3 {
		t.Errorf("expected %d devices, got %d", len(expectedResponse), len(gotResponse))
	}
	for i, device := range expectedResponse {
		if device.ID != gotResponse[i].ID {
			t.Errorf("expected device ID %v at index %d, got device ID %v", device.ID, i, gotResponse[i].ID)
		}
		if device.DeviceToken != gotResponse[i].DeviceToken {
			t.Errorf("expected device token %v at index %d, got device token %v", device.DeviceToken, i, gotResponse[i].DeviceToken)
		}
		if device.Platform != gotResponse[i].Platform {
			t.Errorf("expected device platform %v at index %d, got device platform %v", device.Platform, i, gotResponse[i].Platform)
		}
	}
}

func TestGetUserDeviceToken_Empty(t *testing.T) {
	userID := uuid.New()
	repo := &testUserDeviceRepo{
		getUserDeviceTokenFn: func(ctx context.Context, userID uuid.UUID) ([]db.UserDevice, error) {
			return []db.UserDevice{}, nil
		},
	}
	gotResponse, err := repo.GetUserDeviceToken(context.Background(), userID)
	if err != nil {
		t.Errorf("expected empty user device slice, got %v", err)
	}
	if len(gotResponse) != 0 {
		t.Errorf("expected empty slice of user device, got slice of length %d", len(gotResponse))
	}
}

func TestGetUserDeviceToken_Error(t *testing.T) {
	userID := uuid.New()
	repo := &testUserDeviceRepo{
		getUserDeviceTokenFn: func(ctx context.Context, userID uuid.UUID) ([]db.UserDevice, error) {
			return nil, errors.New("error getting user device token")
		},
	}
	_, err := repo.GetUserDeviceToken(context.Background(), userID)
	if err == nil {
		t.Errorf("expected error, got nil")
	}
}
