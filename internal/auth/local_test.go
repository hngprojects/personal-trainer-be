package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"golang.org/x/crypto/bcrypt"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

var discardLog = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeLocalUserRepo controls behaviour for local signup tests.
type fakeLocalUserRepo struct {
	findUser        *db.User
	findErr         error
	createUserErr   error
	markVerifiedErr error
	roleIDs         auth.RoleIDs
	roleIDsErr      error
}

func (f *fakeLocalUserRepo) FindByEmailAndProvider(_ context.Context, _, _ string) (*db.User, error) {
	return f.findUser, f.findErr
}

func (f *fakeLocalUserRepo) FindByEmail(_ context.Context, _ string) (*db.User, error) {
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

func (f *fakeLocalUserRepo) LookupRoleIDs(_ context.Context, _ uuid.UUID) (auth.RoleIDs, error) {
	return f.roleIDs, f.roleIDsErr
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

// fakeLocalAuthRepo controls atomic consume+verify behaviour.
type fakeLocalAuthRepo struct {
	user *db.User
	err  error
}

func (f *fakeLocalAuthRepo) ConsumeAndMarkVerified(_ context.Context, email, _ string) (*db.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.user != nil {
		return f.user, nil
	}
	return &db.User{ID: uuid.New(), Email: email, AuthProvider: "local", IsActive: true}, nil
}

// fakeMailer captures sent emails.
type fakeMailer struct {
	err error
}

func (m *fakeMailer) SendVerificationCode(_, _ string, _ int) error {
	return m.err
}

func (m *fakeMailer) SendAdminCredentials(_, _ string) error {
	return m.err
}

func (m *fakeMailer) SendTrainerCredentials(_, _ string) error {
	return m.err
}

func (m *fakeMailer) SendAccountSetupLink(_, _, _ string, _ int) error {
	return m.err
}

func (m *fakeMailer) SendPasswordResetCode(_, _ string, _ int) error {
	return m.err
}

func (m *fakeMailer) SendWaitlistConfirmation(_ string) error   { return m.err }
func (m *fakeMailer) SendContactConfirmation(_, _ string) error { return m.err }
func (m *fakeMailer) SendDiscoveryBookingConfirmation(_, _ string, _ time.Time, _, _, _, _ string) error {
	return m.err
}
func (m *fakeMailer) SendDiscoveryBookingAdminNotification(_, _, _ string, _ time.Time, _, _, _, _ string) error {
	return m.err
}
func (m *fakeMailer) SendDiscoveryRescheduleConfirmation(_, _ string, _, _ time.Time, _, _, _, _ string) error {
	return m.err
}
func (m *fakeMailer) SendPaidSessionRescheduleConfirmation(_, _ string, _, _ time.Time, _, _ string) error {
	return m.err
}
func (m *fakeMailer) SendPaidSessionRescheduleTrainerNotification(_, _ string, _, _ time.Time, _, _ string) error {
	return m.err
}
func (m *fakeMailer) SendBookingConfirmation(_, _, _ string, _, _ time.Time, _, _ string) error {
	return m.err
}
func (m *fakeMailer) SendSessionReminder(_, _, _ string, _ time.Time, _, _ string) error {
	return nil
}
func (m *fakeMailer) SendSessionReminderTrainer(_, _, _ string, _ time.Time, _, _ string) error {
	return nil
}

// fakeRateLimiter always allows (or always blocks when allowed=false).
type fakeRateLimiter struct {
	allowed bool
	err     error
}

func (f *fakeRateLimiter) Allow(_ context.Context, _ string) (bool, error) {
	return f.allowed, f.err
}

func (f *fakeRateLimiter) Reset(_ context.Context, _ string) error {
	return nil
}

// countingRateLimiter blocks once the number of Allow calls exceeds maxAllowed.
type countingRateLimiter struct {
	calls      int
	maxAllowed int
}

func (c *countingRateLimiter) Allow(_ context.Context, _ string) (bool, error) {
	c.calls++
	return c.calls <= c.maxAllowed, nil
}

func (c *countingRateLimiter) Reset(_ context.Context, _ string) error {
	return nil
}

func newLocalTestHandler(t *testing.T, users auth.UserRepository, sessions auth.SessionRepository, codes auth.VerificationCodeRepository, localAuth auth.LocalAuthRepository, mailer *fakeMailer) *auth.LocalHandler {
	t.Helper()
	return auth.NewLocalHandler(users, sessions, codes, localAuth, mailer, discardLog, "test-otp-secret",
		&fakeRateLimiter{allowed: true},
		&fakeRateLimiter{allowed: true},
	)
}

func doLocalRequest(t *testing.T, method, path, body string, handlerFn func(*gin.Context)) *httptest.ResponseRecorder {
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

// mustHashPassword is a test helper that bcrypt-hashes a plain password.
func mustHashPassword(t *testing.T, plain string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("mustHashPassword: %v", err)
	}
	return string(hash)
}

// ── VerifyEmail tests ────────────────────────────────────────────────────────

func TestVerifyEmail_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	users := &fakeLocalUserRepo{}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, http.MethodPost, "/auth/verify-email",
		`{"email":"john@example.com","code":"123456"}`, h.VerifyEmail)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data object in response")
	}
	accessToken, ok := data["access_token"].(string)
	if !ok || len(accessToken) == 0 {
		t.Error("expected non-empty access_token string")
	}
	refreshToken, ok := data["refresh_token"].(string)
	if !ok || len(refreshToken) == 0 {
		t.Error("expected non-empty refresh_token string")
	}
}

func TestVerifyEmail_InvalidCode(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{err: auth.ErrNotFound}, &fakeMailer{})

	w := doLocalRequest(t, http.MethodPost, "/auth/verify-email",
		`{"email":"john@example.com","code":"000000"}`, h.VerifyEmail)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVerifyEmail_MissingCode(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, http.MethodPost, "/auth/verify-email",
		`{"email":"john@example.com"}`, h.VerifyEmail)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVerifyEmail_CodeTooShort(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, http.MethodPost, "/auth/verify-email",
		`{"email":"john@example.com","code":"123"}`, h.VerifyEmail)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short code, got %d", w.Code)
	}
}

func TestVerifyEmail_CodeNotDigits(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, http.MethodPost, "/auth/verify-email",
		`{"email":"john@example.com","code":"abc123"}`, h.VerifyEmail)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-digit code, got %d", w.Code)
	}
}

func TestVerifyEmail_InvalidEmail(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, http.MethodPost, "/auth/verify-email",
		`{"email":"notanemail","code":"123456"}`, h.VerifyEmail)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVerifyEmail_InvalidJSON(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, http.MethodPost, "/auth/verify-email", `not json`, h.VerifyEmail)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVerifyEmail_MarkVerifiedError(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{err: errors.New("db error")}, &fakeMailer{})

	w := doLocalRequest(t, http.MethodPost, "/auth/verify-email",
		`{"email":"john@example.com","code":"123456"}`, h.VerifyEmail)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestVerifyEmail_SessionError(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	sessions := &fakeLocalSessionRepo{err: errors.New("session error")}
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, sessions, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, http.MethodPost, "/auth/verify-email",
		`{"email":"john@example.com","code":"123456"}`, h.VerifyEmail)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestVerifyEmail_RateLimited(t *testing.T) {
	verifyLimiter := &countingRateLimiter{maxAllowed: 5}
	h := auth.NewLocalHandler(&fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{err: auth.ErrNotFound}, &fakeMailer{}, discardLog, "test-otp-secret",
		verifyLimiter,
		&fakeRateLimiter{allowed: true},
	)

	// Exhaust the 5 allowed attempts
	for i := 0; i < 5; i++ {
		w := doLocalRequest(t, http.MethodPost, "/auth/verify-email",
			`{"email":"victim@example.com","code":"000000"}`, h.VerifyEmail)
		if w.Code != http.StatusBadRequest {
			t.Errorf("attempt %d: expected 400, got %d", i+1, w.Code)
		}
	}

	// 6th attempt should be rate limited
	w := doLocalRequest(t, http.MethodPost, "/auth/verify-email",
		`{"email":"victim@example.com","code":"000000"}`, h.VerifyEmail)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after exceeding attempts, got %d: %s", w.Code, w.Body.String())
	}
}
