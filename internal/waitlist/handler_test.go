package waitlist_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/internal/waitlist"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mockWaitlistRepo implements waitlist.WaitlistRepository for testing
type mockWaitlistRepo struct {
	addEmailFn   func(ctx context.Context, email string) error
	getAllFn     func(ctx context.Context) ([]db.Waitlist, error)
	getByEmailFn func(ctx context.Context, email string) (*db.Waitlist, error)
}

func (m *mockWaitlistRepo) AddEmail(ctx context.Context, email string) error {
	if m.addEmailFn != nil {
		return m.addEmailFn(ctx, email)
	}
	return nil
}

func (m *mockWaitlistRepo) GetAll(ctx context.Context) ([]db.Waitlist, error) {
	if m.getAllFn != nil {
		return m.getAllFn(ctx)
	}
	return []db.Waitlist{}, nil
}

func (m *mockWaitlistRepo) GetByEmail(ctx context.Context, email string) (*db.Waitlist, error) {
	if m.getByEmailFn != nil {
		return m.getByEmailFn(ctx, email)
	}
	return nil, waitlist.ErrNotFound
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testHandler(repo waitlist.WaitlistRepository) *waitlist.WaitlistHandler {
	return waitlist.NewWaitlistHandler(repo, testLogger())
}

// TestHandleAddWaitlist_Success tests adding email successfully
func TestHandleAddWaitlist_Success(t *testing.T) {
	repo := &mockWaitlistRepo{
		addEmailFn: func(ctx context.Context, email string) error {
			return nil
		},
	}

	handler := testHandler(repo)

	// Create request
	body := map[string]string{
		"email":    "test@example.com",
		"feedback": "great service",
	}
	bodyBytes, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/waitlist", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.HandleAddWaitlist(c)

	// Assertions
	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["status"] != "success" {
		t.Errorf("expected status 'success', got %v", resp["status"])
	}
	if resp["code"] != "CREATED" {
		t.Errorf("expected code 'CREATED', got %v", resp["code"])
	}
}

// TestHandleAddWaitlist_InvalidEmail tests adding with invalid email
func TestHandleAddWaitlist_InvalidEmail(t *testing.T) {
	repo := &mockWaitlistRepo{}
	handler := testHandler(repo)

	tests := []struct {
		name      string
		email     string
		wantCode  int
		wantError string
	}{
		{
			name:      "missing email field",
			email:     "",
			wantCode:  http.StatusBadRequest,
			wantError: "email field is required",
		},
		{
			name:      "invalid email format",
			email:     "not-an-email",
			wantCode:  http.StatusBadRequest,
			wantError: "email field is required",
		},
		{
			name:      "email with spaces",
			email:     "test @example.com",
			wantCode:  http.StatusBadRequest,
			wantError: "email field is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]string{
				"email":    tt.email,
				"feedback": "test",
			}
			bodyBytes, _ := json.Marshal(body)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request, _ = http.NewRequest("POST", "/waitlist", bytes.NewReader(bodyBytes))
			c.Request.Header.Set("Content-Type", "application/json")

			handler.HandleAddWaitlist(c)

			if w.Code != tt.wantCode {
				t.Errorf("expected status %d, got %d", tt.wantCode, w.Code)
			}

			var resp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &resp)

			if resp["status"] != "error" {
				t.Errorf("expected status 'error', got %v", resp["status"])
			}
		})
	}
}

// TestHandleAddWaitlist_EmailNormalization tests email is normalized (lowercase, trimmed)
func TestHandleAddWaitlist_EmailNormalization(t *testing.T) {
	var capturedEmail string

	repo := &mockWaitlistRepo{
		addEmailFn: func(ctx context.Context, email string) error {
			capturedEmail = email
			return nil
		},
	}

	handler := testHandler(repo)

	body := map[string]string{
		"email":    "  TEST@EXAMPLE.COM  ",
		"feedback": "test",
	}
	bodyBytes, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/waitlist", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.HandleAddWaitlist(c)

	if capturedEmail != "test@example.com" {
		t.Errorf("expected email 'test@example.com', got '%s'", capturedEmail)
	}
}

// TestHandleAddWaitlist_RepositoryError tests handling repository errors
func TestHandleAddWaitlist_RepositoryError(t *testing.T) {
	repo := &mockWaitlistRepo{
		addEmailFn: func(ctx context.Context, email string) error {
			return context.DeadlineExceeded
		},
	}

	handler := testHandler(repo)

	body := map[string]string{
		"email":    "test@example.com",
		"feedback": "test",
	}
	bodyBytes, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/waitlist", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.HandleAddWaitlist(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["status"] != "error" {
		t.Errorf("expected status 'error', got %v", resp["status"])
	}
}

// TestHandleGetWaitlist_GetAll tests getting all waitlist entries
func TestHandleGetWaitlist_GetAll(t *testing.T) {
	now := time.Now()
	entries := []db.Waitlist{
		{
			ID:        uuid.New(),
			Email:     "user1@example.com",
			CreatedAt: now,
		},
		{
			ID:        uuid.New(),
			Email:     "user2@example.com",
			CreatedAt: now,
		},
	}

	repo := &mockWaitlistRepo{
		getAllFn: func(ctx context.Context) ([]db.Waitlist, error) {
			return entries, nil
		},
	}

	handler := testHandler(repo)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/waitlist", nil)

	params := api.HandleGetWaitlistParams{}
	handler.HandleGetWaitlist(c, params)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["status"] != "success" {
		t.Errorf("expected status 'success', got %v", resp["status"])
	}

	// Check data is array
	data := resp["data"]
	if data == nil {
		t.Errorf("expected data field, got nil")
	}
}

// TestHandleGetWaitlist_GetByEmail tests getting specific email
func TestHandleGetWaitlist_GetByEmail(t *testing.T) {
	now := time.Now()
	expectedEntry := &db.Waitlist{
		ID:        uuid.New(),
		Email:     "test@example.com",
		CreatedAt: now,
	}

	repo := &mockWaitlistRepo{
		getByEmailFn: func(ctx context.Context, email string) (*db.Waitlist, error) {
			if email == "test@example.com" {
				return expectedEntry, nil
			}
			return nil, waitlist.ErrNotFound
		},
	}

	handler := testHandler(repo)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/waitlist?email=test@example.com", nil)

	email := "test@example.com"
	params := api.HandleGetWaitlistParams{Email: &email}
	handler.HandleGetWaitlist(c, params)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["status"] != "success" {
		t.Errorf("expected status 'success', got %v", resp["status"])
	}
}

// TestHandleGetWaitlist_GetByEmail_NotFound tests getting non-existent email
func TestHandleGetWaitlist_GetByEmail_NotFound(t *testing.T) {
	repo := &mockWaitlistRepo{
		getByEmailFn: func(ctx context.Context, email string) (*db.Waitlist, error) {
			return nil, waitlist.ErrNotFound
		},
	}

	handler := testHandler(repo)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/waitlist?email=notfound@example.com", nil)

	email := "notfound@example.com"
	params := api.HandleGetWaitlistParams{Email: &email}
	handler.HandleGetWaitlist(c, params)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["status"] != "error" {
		t.Errorf("expected status 'error', got %v", resp["status"])
	}
}

// TestHandleGetWaitlist_EmailNormalization tests email parameter is normalized
func TestHandleGetWaitlist_EmailNormalization(t *testing.T) {
	var capturedEmail string

	repo := &mockWaitlistRepo{
		getByEmailFn: func(ctx context.Context, email string) (*db.Waitlist, error) {
			capturedEmail = email
			return nil, waitlist.ErrNotFound
		},
	}

	handler := testHandler(repo)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/waitlist?email=TEST%40EXAMPLE.COM", nil)

	email := "TEST@EXAMPLE.COM"
	params := api.HandleGetWaitlistParams{Email: &email}
	handler.HandleGetWaitlist(c, params)

	if capturedEmail != "test@example.com" {
		t.Errorf("expected email 'test@example.com', got '%s'", capturedEmail)
	}
}

// TestHandleGetWaitlist_RepositoryError tests handling repository errors
func TestHandleGetWaitlist_RepositoryError(t *testing.T) {
	repo := &mockWaitlistRepo{
		getAllFn: func(ctx context.Context) ([]db.Waitlist, error) {
			return nil, context.DeadlineExceeded
		},
	}

	handler := testHandler(repo)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/waitlist", nil)

	params := api.HandleGetWaitlistParams{}
	handler.HandleGetWaitlist(c, params)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["status"] != "error" {
		t.Errorf("expected status 'error', got %v", resp["status"])
	}
}
