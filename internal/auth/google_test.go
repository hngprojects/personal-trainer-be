package auth_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// fakeUserRepo satisfies auth.UserRepository without a real database.
type fakeUserRepo struct {
	user *db.User
	err  error
}

func (f *fakeUserRepo) FindByEmailAndProvider(_ context.Context, _, _ string) (*db.User, error) {
	return f.user, f.err
}

func (f *fakeUserRepo) Create(_ context.Context, email, name, provider string) (*db.User, error) {
	return &db.User{
		ID:           uuid.New(),
		Email:        email,
		Name:         name,
		AuthProvider: provider,
		IsActive:     true,
	}, nil
}

func testHandler(repo auth.UserRepository) *auth.GoogleHandler {
	cfg := &config.Config{
		GoogleClientID:     "test-client-id",
		GoogleClientSecret: "test-client-secret",
		GoogleRedirectURL:  "http://localhost:8080/auth/google/callback",
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return auth.NewGoogleHandler(cfg, repo, nil, log)
}

func TestGoogleLogin_SetsStateCookie(t *testing.T) {
	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)

	h := testHandler(&fakeUserRepo{})
	r.GET("/auth/google", h.HandleGoogleLogin)

	req := httptest.NewRequest(http.MethodGet, "/auth/google", nil)
	r.ServeHTTP(w, req)

	cookies := w.Result().Cookies()
	var found bool
	for _, c := range cookies {
		if c.Name == "oauth_state" && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Error("expected oauth_state cookie to be set")
	}
}

func TestGoogleLogin_RedirectsToGoogle(t *testing.T) {
	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)

	h := testHandler(&fakeUserRepo{})
	r.GET("/auth/google", h.HandleGoogleLogin)

	req := httptest.NewRequest(http.MethodGet, "/auth/google", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTemporaryRedirect {
		t.Errorf("expected 302, got %d", w.Code)
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "accounts.google.com") {
		t.Errorf("expected redirect to accounts.google.com, got %q", location)
	}
}

func TestGoogleCallback_MissingStateCookie(t *testing.T) {
	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)

	h := testHandler(&fakeUserRepo{})
	r.GET("/auth/google/callback", h.HandleGoogleCallback)

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=abc&state=xyz", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when cookie is missing, got %d", w.Code)
	}
}

func TestGoogleCallback_StateMismatch(t *testing.T) {
	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)

	h := testHandler(&fakeUserRepo{})
	r.GET("/auth/google/callback", h.HandleGoogleCallback)

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=abc&state=wrong-state", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "correct-state"})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on state mismatch, got %d", w.Code)
	}
}
