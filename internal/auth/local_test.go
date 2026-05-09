package auth_test

import (
	"context"
	"database/sql"
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
	"golang.org/x/crypto/bcrypt"

	"github.com/hngprojects/personal-trainer-be/internal/auth"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
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

func (m *fakeMailer) SendPasswordResetCode(_, _ string, _ int) error {
	return m.err
}

// fakeLocalRoleRepo controls UserHasRole behaviour for local-auth tests.
type fakeLocalRoleRepo struct {
	hasRole map[string]bool
	err     error
}

func (f *fakeLocalRoleRepo) UserHasRole(_ context.Context, _ uuid.UUID, roleName string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.hasRole[roleName], nil
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
	return auth.NewLocalHandler(users, sessions, codes, localAuth, &fakeLocalRoleRepo{}, mailer, discardLog, "test-otp-secret",
		&fakeRateLimiter{allowed: true},
		&fakeRateLimiter{allowed: true},
		&fakeRateLimiter{allowed: true},
		&fakeRateLimiter{allowed: true},
	)
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

// mustHashPassword is a test helper that bcrypt-hashes a plain password.
func mustHashPassword(t *testing.T, plain string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("mustHashPassword: %v", err)
	}
	return string(hash)
}

// ── Register tests ──────────────────────────────────────────────────────────

func TestRegister_Success(t *testing.T) {
	users := &fakeLocalUserRepo{findErr: auth.ErrNotFound}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

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
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	// Uppercase + whitespace should be accepted and normalised
	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"  JOHN@EXAMPLE.COM  "}`, h.Register)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201 for unnormalized email, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyEmail_NormalizesEmail(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email",
		`{"email":"  JOHN@EXAMPLE.COM  ","code":"123456"}`, h.VerifyEmail)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for unnormalized email, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegister_ExistingActiveUser_SendsNewCode(t *testing.T) {
	// Active users can request a new OTP — this endpoint is signup AND login for email-only auth
	users := &fakeLocalUserRepo{findUser: &db.User{ID: uuid.New(), IsActive: true}, findErr: nil}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"john@example.com"}`, h.Register)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201 for existing active user, got %d", w.Code)
	}
}

func TestRegister_InvalidEmail(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"notanemail"}`, h.Register)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestRegister_MissingEmail(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{}`, h.Register)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestRegister_InvalidJSON(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `not json`, h.Register)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestRegister_UnverifiedUserResendCode(t *testing.T) {
	users := &fakeLocalUserRepo{findUser: &db.User{ID: uuid.New(), IsActive: false}, findErr: nil}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"john@example.com"}`, h.Register)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201 for unverified user resend, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegister_FindUserDBError(t *testing.T) {
	users := &fakeLocalUserRepo{findErr: errors.New("db connection error")}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"john@example.com"}`, h.Register)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on DB error, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegister_RateLimited(t *testing.T) {
	users := &fakeLocalUserRepo{findErr: auth.ErrNotFound}
	registerLimiter := &countingRateLimiter{maxAllowed: 3}
	h := auth.NewLocalHandler(users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeLocalRoleRepo{}, &fakeMailer{}, discardLog, "test-otp-secret",
		&fakeRateLimiter{allowed: true},
		registerLimiter,
		&fakeRateLimiter{allowed: true},
		&fakeRateLimiter{allowed: true},
	)

	// Exhaust the 3 allowed register attempts
	for i := 0; i < 3; i++ {
		w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"flood@example.com"}`, h.Register)
		if w.Code != http.StatusCreated {
			t.Errorf("attempt %d: expected 201, got %d", i+1, w.Code)
		}
	}

	// 4th attempt should be rate limited
	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"flood@example.com"}`, h.Register)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after exceeding register attempts, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegister_DBCreateUserError(t *testing.T) {
	users := &fakeLocalUserRepo{findErr: auth.ErrNotFound, createUserErr: errors.New("db error")}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"john@example.com"}`, h.Register)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestRegister_MailerError(t *testing.T) {
	users := &fakeLocalUserRepo{findErr: auth.ErrNotFound}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{err: errors.New("smtp error")})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"john@example.com"}`, h.Register)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestRegister_CodeCreateError(t *testing.T) {
	users := &fakeLocalUserRepo{findErr: auth.ErrNotFound}
	codes := &fakeCodeRepo{createErr: errors.New("db error")}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, codes, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"john@example.com"}`, h.Register)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestRegister_DeleteCodeError(t *testing.T) {
	users := &fakeLocalUserRepo{findErr: auth.ErrNotFound}
	codes := &fakeCodeRepo{deleteErr: errors.New("delete error")}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, codes, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/register", `{"email":"john@example.com"}`, h.Register)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 when delete fails, got %d", w.Code)
	}
}

// ── VerifyEmail tests ────────────────────────────────────────────────────────

func TestVerifyEmail_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	users := &fakeLocalUserRepo{}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

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

	w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email",
		`{"email":"john@example.com","code":"000000"}`, h.VerifyEmail)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVerifyEmail_MissingCode(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email",
		`{"email":"john@example.com"}`, h.VerifyEmail)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVerifyEmail_CodeTooShort(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email",
		`{"email":"john@example.com","code":"123"}`, h.VerifyEmail)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short code, got %d", w.Code)
	}
}

func TestVerifyEmail_CodeNotDigits(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email",
		`{"email":"john@example.com","code":"abc123"}`, h.VerifyEmail)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-digit code, got %d", w.Code)
	}
}

func TestVerifyEmail_InvalidEmail(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email",
		`{"email":"notanemail","code":"123456"}`, h.VerifyEmail)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVerifyEmail_InvalidJSON(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email", `not json`, h.VerifyEmail)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVerifyEmail_MarkVerifiedError(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{err: errors.New("db error")}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email",
		`{"email":"john@example.com","code":"123456"}`, h.VerifyEmail)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestVerifyEmail_SessionError(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	sessions := &fakeLocalSessionRepo{err: errors.New("session error")}
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, sessions, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/verify-email",
		`{"email":"john@example.com","code":"123456"}`, h.VerifyEmail)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestVerifyEmail_RateLimited(t *testing.T) {
	verifyLimiter := &countingRateLimiter{maxAllowed: 5}
	h := auth.NewLocalHandler(&fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{err: auth.ErrNotFound}, &fakeLocalRoleRepo{}, &fakeMailer{}, discardLog, "test-otp-secret",
		verifyLimiter,
		&fakeRateLimiter{allowed: true},
		&fakeRateLimiter{allowed: true},
		&fakeRateLimiter{allowed: true},
	)

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

// ── SignIn tests ─────────────────────────────────────────────────────────────

func signinBody(email, password string) string {
	return `{"email":"` + email + `","password":"` + password + `"}`
}

func TestSignIn_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	hash := mustHashPassword(t, "correct-password")
	users := &fakeLocalUserRepo{
		findUser: &db.User{
			ID:           uuid.New(),
			Email:        "jane@example.com",
			Name:         "Jane Doe",
			AuthProvider: "local",
			IsActive:     true,
			Password:     sql.NullString{String: hash, Valid: true},
		},
	}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/login",
		signinBody("jane@example.com", "correct-password"), h.SignIn)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	if resp["status"] != "success" {
		t.Errorf("expected status success, got %v", resp["status"])
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data object in response")
	}
	if tok, ok := data["access_token"].(string); !ok || tok == "" {
		t.Error("expected non-empty access_token")
	}
	if tok, ok := data["refresh_token"].(string); !ok || tok == "" {
		t.Error("expected non-empty refresh_token")
	}
	userMap, ok := data["user"].(map[string]any)
	if !ok {
		t.Fatal("expected user object in data")
	}
	if userMap["email"] != "jane@example.com" {
		t.Errorf("expected email jane@example.com, got %v", userMap["email"])
	}
}

func TestSignIn_NormalizesEmail(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	hash := mustHashPassword(t, "secret")
	users := &fakeLocalUserRepo{
		findUser: &db.User{
			ID:           uuid.New(),
			Email:        "jane@example.com",
			AuthProvider: "local",
			IsActive:     true,
			Password:     sql.NullString{String: hash, Valid: true},
		},
	}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})
	w := doLocalRequest(t, h, http.MethodPost, "/auth/login",
		`{"email":"  JANE@EXAMPLE.COM  ","password":"secret"}`, h.SignIn)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for unnormalized email, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSignIn_UserNotFound(t *testing.T) {
	users := &fakeLocalUserRepo{findErr: auth.ErrNotFound}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/login",
		signinBody("nobody@example.com", "password"), h.SignIn)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unknown user, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	msg, _ := resp["message"].(string)
	if strings.Contains(strings.ToLower(msg), "not found") {
		t.Errorf("response message leaks account existence: %q", msg)
	}
}

func TestSignIn_AccountNotVerified(t *testing.T) {
	hash := mustHashPassword(t, "password")
	users := &fakeLocalUserRepo{
		findUser: &db.User{
			ID:           uuid.New(),
			Email:        "unverified@example.com",
			AuthProvider: "local",
			IsActive:     false,
			Password:     sql.NullString{String: hash, Valid: true},
		},
	}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/login",
		signinBody("unverified@example.com", "password"), h.SignIn)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unverified account, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSignIn_MissingEmail(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/login",
		`{"password":"secret"}`, h.SignIn)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing email, got %d", w.Code)
	}
}

func TestSignIn_InvalidEmail(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/login",
		`{"email":"notanemail","password":"secret"}`, h.SignIn)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid email format, got %d", w.Code)
	}
}

func TestSignIn_InvalidJSON(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/login", `not json`, h.SignIn)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed JSON, got %d", w.Code)
	}
}

func TestSignIn_DBError(t *testing.T) {
	users := &fakeLocalUserRepo{findErr: errors.New("connection reset")}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/login",
		signinBody("jane@example.com", "password"), h.SignIn)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on DB error, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSignIn_SessionCreateError(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	hash := mustHashPassword(t, "password")
	users := &fakeLocalUserRepo{
		findUser: &db.User{
			ID:           uuid.New(),
			Email:        "jane@example.com",
			AuthProvider: "local",
			IsActive:     true,
			Password:     sql.NullString{String: hash, Valid: true},
		},
	}
	sessions := &fakeLocalSessionRepo{err: errors.New("session store error")}
	h := newLocalTestHandler(t, users, sessions, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/login",
		signinBody("jane@example.com", "password"), h.SignIn)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 when session creation fails, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSignIn_ResponseShape(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	hash := mustHashPassword(t, "secret")
	uid := uuid.New()
	users := &fakeLocalUserRepo{
		findUser: &db.User{
			ID:           uid,
			Email:        "jane@example.com",
			Name:         "Jane",
			AuthProvider: "local",
			IsActive:     true,
			Password:     sql.NullString{String: hash, Valid: true},
		},
	}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/login",
		signinBody("jane@example.com", "secret"), h.SignIn)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)

	// top-level envelope
	if resp["status"] != "success" {
		t.Errorf("status: want success, got %v", resp["status"])
	}
	if resp["code"] != "OK" {
		t.Errorf("code: want OK, got %v", resp["code"])
	}

	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatal("data field missing or not an object")
	}

	// token fields
	for _, field := range []string{"access_token", "refresh_token", "expires_in"} {
		if _, ok := data[field]; !ok {
			t.Errorf("data.%s is missing", field)
		}
	}

	// user sub-object
	userMap, ok := data["user"].(map[string]any)
	if !ok {
		t.Fatal("data.user missing or not an object")
	}
	for _, field := range []string{"id", "email", "name", "user_type", "profile_complete"} {
		if _, ok := userMap[field]; !ok {
			t.Errorf("data.user.%s is missing", field)
		}
	}
	if userMap["email"] != "jane@example.com" {
		t.Errorf("data.user.email: want jane@example.com, got %v", userMap["email"])
	}
	if userMap["profile_complete"] != true {
		t.Errorf("data.user.profile_complete: want true (name is set), got %v", userMap["profile_complete"])
	}
}

func TestSignIn_ProfileComplete_FalseWhenNoName(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	hash := mustHashPassword(t, "secret")
	users := &fakeLocalUserRepo{
		findUser: &db.User{
			ID:           uuid.New(),
			Email:        "noname@example.com",
			Name:         "", // no name set
			AuthProvider: "local",
			IsActive:     true,
			Password:     sql.NullString{String: hash, Valid: true},
		},
	}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, h, http.MethodPost, "/auth/login",
		signinBody("noname@example.com", "secret"), h.SignIn)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	data := resp["data"].(map[string]any)
	userMap := data["user"].(map[string]any)
	if userMap["profile_complete"] != false {
		t.Errorf("expected profile_complete false when name is empty, got %v", userMap["profile_complete"])
	}
}
