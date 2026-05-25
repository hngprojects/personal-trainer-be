package userdevice_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	userdevice "github.com/hngprojects/personal-trainer-be/internal/user_device"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}
func TestHandleRegisterDevice_Success(t *testing.T) {
	userID := uuid.New()
	mockRepo := &mockUserDeviceRepo{
		registerDeviceFn: func(_ context.Context, uid uuid.UUID, token, platform string) (db.UserDevice, error) {
			return db.UserDevice{
				ID: uuid.New(), UserID: uid, DeviceToken: token, Platform: platform,
			}, nil
		},
	}
	handler := userdevice.NewUserDeviceHandler(
		userdevice.NewUserDeviceService(mockRepo, testLogger()),
		testLogger(),
	)
	body := map[string]string{"device_token": "fcm-token-123", "platform": "android"}
	bodyBytes, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(string(common.ContextKeyUserID), userID)
	c.Request, _ = http.NewRequest("POST", "/register/device", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")
	handler.HandleRegisterDevice(c)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	if resp["code"] != "CREATED" {
		t.Errorf("expected CREATED, got %v", resp["code"])
	}
}
func TestHandleRegisterDevice_MissingDeviceToken(t *testing.T) {
	handler := userdevice.NewUserDeviceHandler(
		userdevice.NewUserDeviceService(&mockUserDeviceRepo{}, testLogger()),
		testLogger(),
	)
	body := map[string]string{"device_token": "", "platform": "android"}
	bodyBytes, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(string(common.ContextKeyUserID), uuid.New())
	c.Request, _ = http.NewRequest("POST", "/register/device", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")
	handler.HandleRegisterDevice(c)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
func TestHandleRegisterDevice_InvalidPlatform(t *testing.T) {
	handler := userdevice.NewUserDeviceHandler(
		userdevice.NewUserDeviceService(&mockUserDeviceRepo{}, testLogger()),
		testLogger(),
	)
	body := map[string]string{"device_token": "token-123", "platform": "windows"}
	bodyBytes, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(string(common.ContextKeyUserID), uuid.New())
	c.Request, _ = http.NewRequest("POST", "/register/device", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")
	handler.HandleRegisterDevice(c)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
func TestHandleRegisterDevice_MissingUserID(t *testing.T) {
	handler := userdevice.NewUserDeviceHandler(
		userdevice.NewUserDeviceService(&mockUserDeviceRepo{}, testLogger()),
		testLogger(),
	)
	body := map[string]string{"device_token": "token-123", "platform": "android"}
	bodyBytes, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/register/device", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")
	handler.HandleRegisterDevice(c)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}
func TestHandleRegisterDevice_InvalidUserID(t *testing.T) {
	handler := userdevice.NewUserDeviceHandler(
		userdevice.NewUserDeviceService(&mockUserDeviceRepo{}, testLogger()),
		testLogger(),
	)
	body := map[string]string{"device_token": "token-123", "platform": "android"}
	bodyBytes, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(string(common.ContextKeyUserID), "not-a-uuid")
	c.Request, _ = http.NewRequest("POST", "/register/device", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")
	handler.HandleRegisterDevice(c)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}
func TestHandleRegisterDevice_ServiceError(t *testing.T) {
	mockRepo := &mockUserDeviceRepo{
		registerDeviceFn: func(_ context.Context, _ uuid.UUID, _, _ string) (db.UserDevice, error) {
			return db.UserDevice{}, io.ErrUnexpectedEOF
		},
	}
	handler := userdevice.NewUserDeviceHandler(
		userdevice.NewUserDeviceService(mockRepo, testLogger()),
		testLogger(),
	)
	body := map[string]string{"device_token": "token-123", "platform": "android"}
	bodyBytes, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(string(common.ContextKeyUserID), uuid.New())
	c.Request, _ = http.NewRequest("POST", "/register/device", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")
	handler.HandleRegisterDevice(c)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}
