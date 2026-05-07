package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/auth"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

var discardLog = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeLocalUserRepo controls behaviour for local signup tests.
type fakeLocalUserRepo struct {
	findUser        *db.User
	findErr         error
	createUserErr   error
	markVerifiedErr error
}

func (f *fakeLocalUserRepo) FindByEmailAndProvider(_ context.Context, _, _ string) (*db.User, error) {
	return f.findUser, f.findErr
}

func (f *fakeLocalUserRepo) Create(_ context.Context, email, name, provider string) (*db.User, error) {
	return &db.User{ID: uuid.New(), Email: email, Name: name, AuthProvider: provider}, nil
}

func (f *fakeLocalUserRepo) CreateEmailUser(_ context.Context, email string) (*db.User, error) {
	if f.createUserErr != nil {
		return nil, f.createUserErr
	}
	return &db.User{ID: uuid.New(), Email: email, AuthProvider: "local"}, nil
}

func (f *fakeLocalUserRepo) MarkVerified(_ context.Context, email string) (*db.User, error) {
	if f.markVerifiedErr != nil {
		return nil, f.markVerifiedErr
	}
	return &db.User{ID: uuid.New(), Email: email, AuthProvider: "local", IsActive: true}, nil
}

// fakeCodeRepo controls verification code behaviour.
type fakeCodeRepo struct {
	consumeErr error
	createErr  error
	deleteErr  error
}

func (f *fakeCodeRepo) Create(_ context.Context, _, _ string, _ time.Time) error {
	return f.createErr
}

func (f *fakeCodeRepo) ConsumeByEmailAndCode(_ context.Context, _, _ string) (*db.VerificationCode, error) {
	if f.consumeErr != nil {
		return nil, f.consumeErr
	}
	return &db.VerificationCode{ID: uuid.New()}, nil
}

func (f *fakeCodeRepo) DeleteByEmail(_ context.Context, _ string) error {
	return f.deleteErr
}

// fakeLocalSessionRepo controls session creation behaviour.
type fakeLocalSessionRepo struct {
	err error
}

func (f *fakeLocalSessionRepo) Create(_ context.Context, _ uuid.UUID, _ string, _ time.Time) (*db.Session, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &db.Session{ID: uuid.New()}, nil
}

// fakeMailer captures sent emails.
type fakeMailer struct {
	err error
}

func (m *fakeMailer) Send(_, _, _ string) error {
	return m.err
}

func newLocalTestHandler(users auth.UserRepository, sessions auth.SessionRepository, codes auth.VerificationCodeRepository, mailer *fakeMailer) *auth.LocalHandler {
	return auth.NewLocalHandler(users, sessions, codes, mailer, discardLog)
}

func doLocalRequest(t *testing.T, h *auth.LocalHandler, method, path, body string, handlerFn func(*gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.Handle(method, path, handlerFn)
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp
}

// ── Register tests ──────────────────────────────────────────────────────────

func TestRegister_Success(t *testing.T) {
	users := &fakeLocalUserRepo{findErr: auth.ErrNotFound}
	h := newLocalTestHandler(users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"john@example.com"}`, h.Register)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	if resp["status"] != "success" {
		t.Errorf("expected status success, got %v", resp["status"])
	}
}

func TestRegister_NormalizesEmail(t *testing.T) {
	users := &fakeLocalUserRepo{findErr: auth.ErrNotFound}
	h := newLocalTestHandler(users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeMailer{})

	// Uppercase + whitespace should be accepted and normalised
	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"  JOHN@EXAMPLE.COM  "}`, h.Register)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201 for unnormalized email, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyEmail_NormalizesEmail(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	h := newLocalTestHandler(&fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email",
		`{"email":"  JOHN@EXAMPLE.COM  ","code":"123456"}`, h.VerifyEmail)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for unnormalized email, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegister_ExistingActiveUser_SendsNewCode(t *testing.T) {
	// Active users can request a new OTP — this endpoint is signup AND login for email-only auth
	users := &fakeLocalUserRepo{findUser: &db.User{ID: uuid.New(), IsActive: true}, findErr: nil}
	h := newLocalTestHandler(users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"john@example.com"}`, h.Register)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201 for existing active user, got %d", w.Code)
	}
}

func TestRegister_InvalidEmail(t *testing.T) {
	h := newLocalTestHandler(&fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"notanemail"}`, h.Register)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestRegister_MissingEmail(t *testing.T) {
	h := newLocalTestHandler(&fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{}`, h.Register)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestRegister_InvalidJSON(t *testing.T) {
	h := newLocalTestHandler(&fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `not json`, h.Register)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestRegister_UnverifiedUserResendCode(t *testing.T) {
	users := &fakeLocalUserRepo{findUser: &db.User{ID: uuid.New(), IsActive: false}, findErr: nil}
	h := newLocalTestHandler(users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"john@example.com"}`, h.Register)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201 for unverified user resend, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegister_DBCreateUserError(t *testing.T) {
	users := &fakeLocalUserRepo{findErr: auth.ErrNotFound, createUserErr: errors.New("db error")}
	h := newLocalTestHandler(users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"john@example.com"}`, h.Register)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestRegister_MailerError(t *testing.T) {
	users := &fakeLocalUserRepo{findErr: auth.ErrNotFound}
	h := newLocalTestHandler(users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeMailer{err: errors.New("smtp error")})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"john@example.com"}`, h.Register)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestRegister_CodeCreateError(t *testing.T) {
	users := &fakeLocalUserRepo{findErr: auth.ErrNotFound}
	codes := &fakeCodeRepo{createErr: errors.New("db error")}
	h := newLocalTestHandler(users, &fakeLocalSessionRepo{}, codes, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"john@example.com"}`, h.Register)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// ── VerifyEmail tests ────────────────────────────────────────────────────────

func TestVerifyEmail_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	users := &fakeLocalUserRepo{}
	h := newLocalTestHandler(users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email",
		`{"email":"john@example.com","code":"123456"}`, h.VerifyEmail)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data object in response")
	}
	if data["access_token"] == "" {
		t.Error("expected non-empty access_token")
	}
	if data["refresh_token"] == "" {
		t.Error("expected non-empty refresh_token")
	}
}

func TestVerifyEmail_InvalidCode(t *testing.T) {
	users := &fakeLocalUserRepo{}
	codes := &fakeCodeRepo{consumeErr: auth.ErrNotFound}
	h := newLocalTestHandler(users, &fakeLocalSessionRepo{}, codes, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email",
		`{"email":"john@example.com","code":"000000"}`, h.VerifyEmail)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVerifyEmail_MissingCode(t *testing.T) {
	h := newLocalTestHandler(&fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email",
		`{"email":"john@example.com"}`, h.VerifyEmail)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVerifyEmail_InvalidEmail(t *testing.T) {
	h := newLocalTestHandler(&fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email",
		`{"email":"notanemail","code":"123456"}`, h.VerifyEmail)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVerifyEmail_InvalidJSON(t *testing.T) {
	h := newLocalTestHandler(&fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email", `not json`, h.VerifyEmail)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVerifyEmail_MarkVerifiedError(t *testing.T) {
	users := &fakeLocalUserRepo{markVerifiedErr: errors.New("db error")}
	h := newLocalTestHandler(users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email",
		`{"email":"john@example.com","code":"123456"}`, h.VerifyEmail)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestVerifyEmail_SessionError(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	users := &fakeLocalUserRepo{}
	sessions := &fakeLocalSessionRepo{err: errors.New("session error")}
	h := newLocalTestHandler(users, sessions, &fakeCodeRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email",
		`{"email":"john@example.com","code":"123456"}`, h.VerifyEmail)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestVerifyEmail_RateLimited(t *testing.T) {
	codes := &fakeCodeRepo{consumeErr: auth.ErrNotFound}
	h := newLocalTestHandler(&fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, codes, &fakeMailer{})

	// Exhaust the 5 allowed attempts
	for i := 0; i < 5; i++ {
		w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email",
			`{"email":"victim@example.com","code":"000000"}`, h.VerifyEmail)
		if w.Code != http.StatusBadRequest {
			t.Errorf("attempt %d: expected 400, got %d", i+1, w.Code)
		}
	}

	// 6th attempt should be rate limited
	w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email",
		`{"email":"victim@example.com","code":"000000"}`, h.VerifyEmail)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after exceeding attempts, got %d: %s", w.Code, w.Body.String())
	}
}
