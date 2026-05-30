package notification_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/internal/notification"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	fcmnotif "github.com/hngprojects/personal-trainer-be/pkg/notification"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestHandleSendNotification_Success(t *testing.T) {
	userID := uuid.New()
	repo := &mockRepository{
		getUserRoleByUserIDFn: func(_ context.Context, _ uuid.UUID) (string, error) {
			return "client", nil
		},
		createNotificationWithTypeFn: func(_ context.Context, args db.CreateNotificationWithTypeParams) (db.Notification, error) {
			return db.Notification{
				ID: uuid.New(), UserID: args.UserID, Title: args.Title,
				Message: args.Message, Status: "pending",
			}, nil
		},
		getUserDeviceTokenFn: func(_ context.Context, _ uuid.UUID) (*[]db.UserDevice, error) {
			return &[]db.UserDevice{}, nil
		},
	}
	handler := notification.NewNotificationHandler(
		notification.NewNotificationService(repo, fcmnotif.NewPushNotification([]byte{}, "", nil, testLogger()), &mockWSHub{}, testLogger()),
		testLogger(),
	)

	body := map[string]string{
		"title":           "Test Title",
		"message":         "Test Message",
		"idempotency_key": "idem-123",
	}
	bodyBytes, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(string(common.ContextKeyUserID), userID)
	c.Request, _ = http.NewRequest("POST", "/notifications/send", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.SendNotificationToUser(c)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", w.Code)
	}
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	if resp["code"] != "CREATED" {
		t.Errorf("expected CREATED, got %v", resp["code"])
	}
}

func TestHandleSendNotification_MissingTitle(t *testing.T) {
	handler := notification.NewNotificationHandler(
		notification.NewNotificationService(&mockRepository{}, fcmnotif.NewPushNotification([]byte{}, "", nil, testLogger()), &mockWSHub{}, testLogger()),
		testLogger(),
	)

	body := map[string]string{"title": "", "message": "Msg", "idempotency_key": "key"}
	bodyBytes, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(string(common.ContextKeyUserID), uuid.New())
	c.Request, _ = http.NewRequest("POST", "/notifications/send", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.SendNotificationToUser(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSendNotification_MissingMessage(t *testing.T) {
	handler := notification.NewNotificationHandler(
		notification.NewNotificationService(&mockRepository{}, fcmnotif.NewPushNotification([]byte{}, "", nil, testLogger()), &mockWSHub{}, testLogger()),
		testLogger(),
	)

	body := map[string]string{"title": "Title", "message": "", "idempotency_key": "key"}
	bodyBytes, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(string(common.ContextKeyUserID), uuid.New())
	c.Request, _ = http.NewRequest("POST", "/notifications/send", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.SendNotificationToUser(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSendNotification_MissingIdempotencyKey(t *testing.T) {
	handler := notification.NewNotificationHandler(
		notification.NewNotificationService(&mockRepository{}, fcmnotif.NewPushNotification([]byte{}, "", nil, testLogger()), &mockWSHub{}, testLogger()),
		testLogger(),
	)

	body := map[string]string{"title": "Title", "message": "Msg", "idempotency_key": ""}
	bodyBytes, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(string(common.ContextKeyUserID), uuid.New())
	c.Request, _ = http.NewRequest("POST", "/notifications/send", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.SendNotificationToUser(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSendNotification_Unauthorized(t *testing.T) {
	handler := notification.NewNotificationHandler(
		notification.NewNotificationService(&mockRepository{}, fcmnotif.NewPushNotification([]byte{}, "", nil, testLogger()), &mockWSHub{}, testLogger()),
		testLogger(),
	)

	body := map[string]string{"title": "Title", "message": "Msg", "idempotency_key": "key"}
	bodyBytes, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/notifications/send", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.SendNotificationToUser(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestHandleSendNotification_InvalidUserID(t *testing.T) {
	handler := notification.NewNotificationHandler(
		notification.NewNotificationService(&mockRepository{}, fcmnotif.NewPushNotification([]byte{}, "", nil, testLogger()), &mockWSHub{}, testLogger()),
		testLogger(),
	)

	body := map[string]string{"title": "Title", "message": "Msg", "idempotency_key": "key"}
	bodyBytes, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(string(common.ContextKeyUserID), "not-a-uuid")
	c.Request, _ = http.NewRequest("POST", "/notifications/send", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.SendNotificationToUser(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestHandleSendNotification_ServiceError(t *testing.T) {
	repo := &mockRepository{
		getUserRoleByUserIDFn: func(_ context.Context, _ uuid.UUID) (string, error) {
			return "client", nil
		},
		createNotificationWithTypeFn: func(_ context.Context, _ db.CreateNotificationWithTypeParams) (db.Notification, error) {
			return db.Notification{}, errors.New("db error")
		},
	}
	handler := notification.NewNotificationHandler(
		notification.NewNotificationService(repo, fcmnotif.NewPushNotification([]byte{}, "", nil, testLogger()), &mockWSHub{}, testLogger()),
		testLogger(),
	)

	body := map[string]string{"title": "Title", "message": "Msg", "idempotency_key": "key"}
	bodyBytes, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(string(common.ContextKeyUserID), uuid.New())
	c.Request, _ = http.NewRequest("POST", "/notifications/send", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.SendNotificationToUser(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleGetUserNotifications_Success(t *testing.T) {
	userID := uuid.New()
	repo := &mockRepository{
		getUserNotificationFn: func(_ context.Context, _ uuid.UUID) (*[]db.Notification, error) {
			return &[]db.Notification{
				{ID: uuid.New(), UserID: userID, Title: "Notif 1", Message: "Msg 1", Status: "sent"},
			}, nil
		},
	}
	handler := notification.NewNotificationHandler(
		notification.NewNotificationService(repo, fcmnotif.NewPushNotification([]byte{}, "", nil, testLogger()), &mockWSHub{}, testLogger()),
		testLogger(),
	)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(string(common.ContextKeyUserID), userID)
	c.Request, _ = http.NewRequest("GET", "/notifications", nil)

	handler.GetUserNotifications(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	if resp["code"] != "OK" {
		t.Errorf("expected OK, got %v", resp["code"])
	}
}

func TestHandleGetUserNotifications_Unauthorized(t *testing.T) {
	handler := notification.NewNotificationHandler(
		notification.NewNotificationService(&mockRepository{}, fcmnotif.NewPushNotification([]byte{}, "", nil, testLogger()), &mockWSHub{}, testLogger()),
		testLogger(),
	)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/notifications", nil)

	handler.GetUserNotifications(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestHandleGetUserNotifications_ServiceError(t *testing.T) {
	repo := &mockRepository{
		getUserNotificationFn: func(_ context.Context, _ uuid.UUID) (*[]db.Notification, error) {
			return nil, errors.New("db error")
		},
	}
	handler := notification.NewNotificationHandler(
		notification.NewNotificationService(repo, fcmnotif.NewPushNotification([]byte{}, "", nil, testLogger()), &mockWSHub{}, testLogger()),
		testLogger(),
	)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(string(common.ContextKeyUserID), uuid.New())
	c.Request, _ = http.NewRequest("GET", "/notifications", nil)

	handler.GetUserNotifications(c)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}
