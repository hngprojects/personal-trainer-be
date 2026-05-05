package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/handlers"
	"github.com/hngprojects/personal-trainer-be/internal/models"
	"github.com/hngprojects/personal-trainer-be/internal/service"
)

func init() {
	gin.SetMode(gin.TestMode)
}

type mockAuthService struct {
	initiateSignUpFn func(ctx context.Context, email string) error
	verifyCodeFn     func(ctx context.Context, email, code string) error
	completeSignUpFn func(ctx context.Context, email, name, code, password string) (*models.Session, error)
	signInFn         func(ctx context.Context, email, password string) (*models.Session, *models.User, error)
}

func (m *mockAuthService) InitiateSignUp(ctx context.Context, email string) error {
	if m.initiateSignUpFn != nil {
		return m.initiateSignUpFn(ctx, email)
	}
	return nil
}
func (m *mockAuthService) VerifyCode(ctx context.Context, email, code string) error {
	if m.verifyCodeFn != nil {
		return m.verifyCodeFn(ctx, email, code)
	}
	return nil
}
func (m *mockAuthService) CompleteSignUp(ctx context.Context, email, name, code, password string) (*models.Session, error) {
	if m.completeSignUpFn != nil {
		return m.completeSignUpFn(ctx, email, name, code, password)
	}
	return nil, nil
}
func (m *mockAuthService) SignIn(ctx context.Context, email, password string) (*models.Session, *models.User, error) {
	if m.signInFn != nil {
		return m.signInFn(ctx, email, password)
	}
	return nil, nil, nil
}

func newTestRouter(svc *mockAuthService) *gin.Engine {
	r := gin.New()
	h := handlers.NewAuthHandler(svc, &config.Config{})
	r.POST("/auth/register", h.InitiateSignUp)
	r.POST("/auth/register/verify", h.VerifyCode)
	r.POST("/auth/register/complete", h.CompleteSignUp)
	r.POST("/auth/login", h.SignIn)
	return r
}

func assertErrorCode(t *testing.T, w *httptest.ResponseRecorder, code string) {
	t.Helper()
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error.Code != code {
		t.Errorf("expected error code %q, got %q", code, resp.Error.Code)
	}
}

// --- InitiateSignUp tests ---

func TestInitiateSignUp_MissingEmail(t *testing.T) {
	r := newTestRouter(&mockAuthService{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/auth/register", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	assertErrorCode(t, w, "VALIDATION_FAILED")
}

func TestInitiateSignUp_InvalidEmail(t *testing.T) {
	r := newTestRouter(&mockAuthService{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/auth/register", bytes.NewBufferString(`{"email":"not-an-email"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	assertErrorCode(t, w, "VALIDATION_FAILED")
}

func TestInitiateSignUp_EmailAlreadyExists(t *testing.T) {
	svc := &mockAuthService{
		initiateSignUpFn: func(_ context.Context, _ string) error {
			return service.ErrEmailAlreadyExists
		},
	}
	r := newTestRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/auth/register", bytes.NewBufferString(`{"email":"user@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
	assertErrorCode(t, w, "EMAIL_EXISTS")
}

func TestInitiateSignUp_Success(t *testing.T) {
	svc := &mockAuthService{
		initiateSignUpFn: func(_ context.Context, _ string) error { return nil },
	}
	r := newTestRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/auth/register", bytes.NewBufferString(`{"email":"user@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- VerifyCode tests ---

func TestVerifyCode_MissingFields(t *testing.T) {
	r := newTestRouter(&mockAuthService{})
	cases := []struct {
		name string
		body string
	}{
		{"missing both", `{}`},
		{"missing code", `{"email":"user@example.com"}`},
		{"missing email", `{"code":"123456"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodPost, "/auth/register/verify", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", w.Code)
			}
			assertErrorCode(t, w, "VALIDATION_FAILED")
		})
	}
}

func TestVerifyCode_InvalidCode(t *testing.T) {
	svc := &mockAuthService{
		verifyCodeFn: func(_ context.Context, _, _ string) error {
			return service.ErrInvalidCode
		},
	}
	r := newTestRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/auth/register/verify", bytes.NewBufferString(`{"email":"user@example.com","code":"000000"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	assertErrorCode(t, w, "INVALID_CODE")
}

func TestVerifyCode_Success(t *testing.T) {
	svc := &mockAuthService{
		verifyCodeFn: func(_ context.Context, _, _ string) error { return nil },
	}
	r := newTestRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/auth/register/verify", bytes.NewBufferString(`{"email":"user@example.com","code":"123456"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- CompleteSignUp tests ---

func TestCompleteSignUp_MissingFields(t *testing.T) {
	r := newTestRouter(&mockAuthService{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/auth/register/complete", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	assertErrorCode(t, w, "VALIDATION_FAILED")
}

func TestCompleteSignUp_WeakPassword(t *testing.T) {
	svc := &mockAuthService{
		completeSignUpFn: func(_ context.Context, _, _, _, _ string) (*models.Session, error) {
			return nil, service.ErrWeakPassword
		},
	}
	r := newTestRouter(svc)
	w := httptest.NewRecorder()
	body := `{"email":"user@example.com","name":"Test","code":"123456","password":"weak"}`
	req, _ := http.NewRequest(http.MethodPost, "/auth/register/complete", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	assertErrorCode(t, w, "WEAK_PASSWORD")
}

func TestCompleteSignUp_InvalidCode(t *testing.T) {
	svc := &mockAuthService{
		completeSignUpFn: func(_ context.Context, _, _, _, _ string) (*models.Session, error) {
			return nil, service.ErrInvalidCode
		},
	}
	r := newTestRouter(svc)
	w := httptest.NewRecorder()
	body := `{"email":"user@example.com","name":"Test","code":"000000","password":"Secret123"}`
	req, _ := http.NewRequest(http.MethodPost, "/auth/register/complete", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	assertErrorCode(t, w, "INVALID_CODE")
}

func TestCompleteSignUp_Success(t *testing.T) {
	svc := &mockAuthService{
		completeSignUpFn: func(_ context.Context, _, _, _, _ string) (*models.Session, error) {
			return &models.Session{ID: "550e8400-e29b-41d4-a716-446655440000"}, nil
		},
	}
	r := newTestRouter(svc)
	w := httptest.NewRecorder()
	body := `{"email":"user@example.com","name":"Test","code":"123456","password":"Secret123"}`
	req, _ := http.NewRequest(http.MethodPost, "/auth/register/complete", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", w.Code)
	}
}

// --- SignIn tests ---

func TestSignIn_MissingFields(t *testing.T) {
	r := newTestRouter(&mockAuthService{})
	cases := []struct {
		name string
		body string
	}{
		{"missing both", `{}`},
		{"missing password", `{"email":"user@example.com"}`},
		{"missing email", `{"password":"Secret123"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", w.Code)
			}
			assertErrorCode(t, w, "VALIDATION_FAILED")
		})
	}
}

func TestSignIn_InvalidCredentials(t *testing.T) {
	svc := &mockAuthService{
		signInFn: func(_ context.Context, _, _ string) (*models.Session, *models.User, error) {
			return nil, nil, service.ErrInvalidCredentials
		},
	}
	r := newTestRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(`{"email":"user@example.com","password":"Wrong123"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	assertErrorCode(t, w, "INVALID_CREDENTIALS")
}

func TestSignIn_AccountInactive(t *testing.T) {
	svc := &mockAuthService{
		signInFn: func(_ context.Context, _, _ string) (*models.Session, *models.User, error) {
			return nil, nil, service.ErrAccountNotActive
		},
	}
	r := newTestRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(`{"email":"user@example.com","password":"Secret123"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	assertErrorCode(t, w, "ACCOUNT_INACTIVE")
}

func TestSignIn_ServiceError(t *testing.T) {
	svc := &mockAuthService{
		signInFn: func(_ context.Context, _, _ string) (*models.Session, *models.User, error) {
			return nil, nil, errors.New("db error")
		},
	}
	r := newTestRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(`{"email":"user@example.com","password":"Secret123"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	assertErrorCode(t, w, "INTERNAL_ERROR")
}

func TestSignIn_Success(t *testing.T) {
	svc := &mockAuthService{
		signInFn: func(_ context.Context, _, _ string) (*models.Session, *models.User, error) {
			return &models.Session{ID: "550e8400-e29b-41d4-a716-446655440000"},
				&models.User{ID: "550e8400-e29b-41d4-a716-446655440001", Email: "user@example.com", Name: "Test"},
				nil
		},
	}
	r := newTestRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(`{"email":"user@example.com","password":"Secret123"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
