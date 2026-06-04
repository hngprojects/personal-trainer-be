package auth_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/apple"
)

// fakeAppleVerifier returns whatever's set on it — never touches Apple.
type fakeAppleVerifier struct {
	claims *apple.Claims
	err    error
}

func (f *fakeAppleVerifier) Verify(_ context.Context, _ string) (*apple.Claims, error) {
	return f.claims, f.err
}

// fakeAppleUserRepo lets a test simulate first-time vs returning sign-ins.
type fakeAppleUserRepo struct {
	existing      *db.User
	notFound      bool
	createErr     error
	lookupErr     error
	createdEmail  string
	createdName   string
	createdSub    string
	createCalls   int
	findSubCalls  int
	lookupRoleErr error
}

func (f *fakeAppleUserRepo) FindByAppleSub(_ context.Context, _ string) (*db.User, error) {
	f.findSubCalls++
	if f.lookupErr != nil {
		return nil, f.lookupErr
	}
	if f.notFound {
		return nil, auth.ErrNotFound
	}
	return f.existing, nil
}

func (f *fakeAppleUserRepo) CreateAppleUser(_ context.Context, email, name, sub string) (*db.User, error) {
	f.createCalls++
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.createdEmail = email
	f.createdName = name
	f.createdSub = sub
	return &db.User{
		ID:           uuid.New(),
		Email:        email,
		Name:         name,
		AuthProvider: "apple",
		AppleUserID:  sql.NullString{String: sub, Valid: sub != ""},
		IsActive:     true,
	}, nil
}

func (f *fakeAppleUserRepo) FindByEmail(_ context.Context, _ string) (*db.User, error) {
	return nil, auth.ErrNotFound
}
func (f *fakeAppleUserRepo) FindByEmailAndProvider(_ context.Context, _, _ string) (*db.User, error) {
	return nil, auth.ErrNotFound
}
func (f *fakeAppleUserRepo) Create(_ context.Context, _, _, _ string) (*db.User, error) {
	return nil, nil
}
func (f *fakeAppleUserRepo) CreateEmailUser(_ context.Context, _ string) (*db.User, error) {
	return nil, nil
}
func (f *fakeAppleUserRepo) MarkVerified(_ context.Context, _ string) (*db.User, error) {
	return nil, nil
}
func (f *fakeAppleUserRepo) LookupRoleIDs(_ context.Context, _ uuid.UUID) (auth.RoleIDs, error) {
	return auth.RoleIDs{}, f.lookupRoleErr
}

type fakeAppleSessionRepo struct {
	err   error
	calls int
}

func (f *fakeAppleSessionRepo) Create(_ context.Context, _ uuid.UUID, _ string, _ time.Time) (*db.Session, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &db.Session{ID: uuid.New()}, nil
}

func newAppleTestHandler(t *testing.T, users *fakeAppleUserRepo, sessions *fakeAppleSessionRepo, ver auth.AppleVerifier) *auth.AppleHandler {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-secret")
	cfg := &config.Config{
		AppleSignInBundleIDs: []string{"com.fitcal.app"},
	}
	return auth.NewAppleHandler(cfg, users, sessions, ver, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func postAppleSignIn(t *testing.T, h *auth.AppleHandler, body any) *httptest.ResponseRecorder {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r := gin.New()
	r.POST("/auth/apple", h.SignIn)
	r.ServeHTTP(w, req)
	return w
}

func TestAppleSignIn_FirstTimeCreatesUser(t *testing.T) {
	users := &fakeAppleUserRepo{notFound: true}
	sessions := &fakeAppleSessionRepo{}
	ver := &fakeAppleVerifier{claims: &apple.Claims{
		Sub:           "001234.abc.xyz",
		Email:         "first@example.com",
		EmailVerified: true,
	}}
	h := newAppleTestHandler(t, users, sessions, ver)

	body := map[string]any{
		"id_token": "any-string-the-fake-verifier-ignores",
		"user":     map[string]any{"name": "Jane Appleseed"},
	}
	w := postAppleSignIn(t, h, body)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body: %s", w.Code, w.Body.String())
	}
	if users.createCalls != 1 {
		t.Errorf("expected exactly one create call, got %d", users.createCalls)
	}
	if users.createdSub != "001234.abc.xyz" {
		t.Errorf("created sub: got %q", users.createdSub)
	}
	if users.createdName != "Jane Appleseed" {
		t.Errorf("created name: got %q", users.createdName)
	}
	if sessions.calls != 1 {
		t.Errorf("expected session create, got %d calls", sessions.calls)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, _ := resp["data"].(map[string]any)
	if data == nil {
		t.Fatalf("missing data: %v", resp)
	}
	if data["is_new_user"] != true {
		t.Errorf("is_new_user should be true: %v", data["is_new_user"])
	}
	if data["access_token"] == "" {
		t.Errorf("access_token should be present")
	}
}

func TestAppleSignIn_ReturningFindsBySub(t *testing.T) {
	existing := &db.User{
		ID:           uuid.New(),
		Email:        "first@example.com",
		Name:         "Existing User",
		AuthProvider: "apple",
		AppleUserID:  sql.NullString{String: "001234.abc.xyz", Valid: true},
		IsActive:     true,
		Role:         "client",
	}
	users := &fakeAppleUserRepo{existing: existing}
	sessions := &fakeAppleSessionRepo{}
	// Returning sign-in: email/name claims omitted.
	ver := &fakeAppleVerifier{claims: &apple.Claims{Sub: "001234.abc.xyz"}}
	h := newAppleTestHandler(t, users, sessions, ver)

	w := postAppleSignIn(t, h, map[string]any{"id_token": "x"})

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body: %s", w.Code, w.Body.String())
	}
	if users.createCalls != 0 {
		t.Errorf("returning user must not create — got %d create calls", users.createCalls)
	}
	if users.findSubCalls != 1 {
		t.Errorf("expected one FindByAppleSub call, got %d", users.findSubCalls)
	}

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	data, _ := resp["data"].(map[string]any)
	if data["is_new_user"] != false {
		t.Errorf("is_new_user should be false for returning user: %v", data["is_new_user"])
	}
}

func TestAppleSignIn_VerifierRejection401(t *testing.T) {
	users := &fakeAppleUserRepo{}
	sessions := &fakeAppleSessionRepo{}
	ver := &fakeAppleVerifier{err: errors.New("bad signature")}
	h := newAppleTestHandler(t, users, sessions, ver)

	w := postAppleSignIn(t, h, map[string]any{"id_token": "garbage"})

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401, body: %s", w.Code, w.Body.String())
	}
	if users.findSubCalls != 0 || users.createCalls != 0 {
		t.Errorf("must not touch repo on verifier failure (find=%d create=%d)", users.findSubCalls, users.createCalls)
	}
}

func TestAppleSignIn_EmptyIDToken400(t *testing.T) {
	users := &fakeAppleUserRepo{}
	sessions := &fakeAppleSessionRepo{}
	ver := &fakeAppleVerifier{}
	h := newAppleTestHandler(t, users, sessions, ver)

	w := postAppleSignIn(t, h, map[string]any{"id_token": "   "})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400, body: %s", w.Code, w.Body.String())
	}
}

func TestAppleSignIn_PrivateRelayEmailStoredVerbatim(t *testing.T) {
	users := &fakeAppleUserRepo{notFound: true}
	sessions := &fakeAppleSessionRepo{}
	ver := &fakeAppleVerifier{claims: &apple.Claims{
		Sub:            "001234.abc.xyz",
		Email:          "xyz123@privaterelay.appleid.com",
		EmailVerified:  true,
		IsPrivateEmail: true,
	}}
	h := newAppleTestHandler(t, users, sessions, ver)

	w := postAppleSignIn(t, h, map[string]any{"id_token": "x"})

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, body: %s", w.Code, w.Body.String())
	}
	if users.createdEmail != "xyz123@privaterelay.appleid.com" {
		t.Errorf("private-relay email must be stored verbatim, got %q", users.createdEmail)
	}
}

func TestAppleSignIn_NoVerifier503(t *testing.T) {
	users := &fakeAppleUserRepo{}
	sessions := &fakeAppleSessionRepo{}
	h := newAppleTestHandler(t, users, sessions, nil)

	w := postAppleSignIn(t, h, map[string]any{"id_token": "x"})

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503, body: %s", w.Code, w.Body.String())
	}
}

func TestAppleSignIn_SessionCreateFailure500(t *testing.T) {
	users := &fakeAppleUserRepo{notFound: true}
	sessions := &fakeAppleSessionRepo{err: errors.New("db down")}
	ver := &fakeAppleVerifier{claims: &apple.Claims{Sub: "001234.abc.xyz", Email: "x@y.com"}}
	h := newAppleTestHandler(t, users, sessions, ver)

	w := postAppleSignIn(t, h, map[string]any{"id_token": "x"})

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d want 500, body: %s", w.Code, w.Body.String())
	}
}
