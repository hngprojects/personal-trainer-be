package middleware_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/middleware"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

// fakeAuthUsers stubs out auth.UserRepository — only ListRoleNames matters for
// these tests; the rest are no-ops to satisfy the interface.
type fakeAuthUsers struct {
	roles []string
	err   error
}

func (f *fakeAuthUsers) FindByEmail(_ context.Context, _ string) (*db.User, error) {
	return nil, nil
}
func (f *fakeAuthUsers) Create(_ context.Context, _, _ string) (*db.User, error) {
	return nil, nil
}
func (f *fakeAuthUsers) CreateLocal(_ context.Context, _, _, _ string) (*db.User, error) {
	return nil, nil
}
func (f *fakeAuthUsers) UpdateLastLogin(_ context.Context, _ uuid.UUID) error {
	return nil
}
func (f *fakeAuthUsers) ListRoleNames(_ context.Context, _ uuid.UUID) ([]string, error) {
	return f.roles, f.err
}
func (f *fakeAuthUsers) AssignRole(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}
func (f *fakeAuthUsers) WithTx(_ *sql.Tx) auth.UserRepository {
	return f
}

// --- AuthMiddleware -------------------------------------------------------

func TestAuthMiddleware_ValidToken_SetsUserID(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	uid := uuid.New()
	tok, err := auth.GenerateJWTToken(uid.String(), auth.AccessToken)
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	var got uuid.UUID
	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/x", middleware.AuthMiddleware(), func(c *gin.Context) {
		got = c.MustGet("user_id").(uuid.UUID)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got != uid {
		t.Errorf("user_id = %v, want %v", got, uid)
	}
}

func TestAuthMiddleware_NoHeader_401(t *testing.T) {
	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/x", middleware.AuthMiddleware(), func(_ *gin.Context) {})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_BadFormat_401(t *testing.T) {
	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/x", middleware.AuthMiddleware(), func(_ *gin.Context) {})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "NotBearer abc.def.ghi")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_InvalidToken_401(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/x", middleware.AuthMiddleware(), func(_ *gin.Context) {})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-jwt")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// --- RequireRole ----------------------------------------------------------

func TestRequireRole_AllowsMatchingRole(t *testing.T) {
	users := &fakeAuthUsers{roles: []string{"client", "super_admin"}}

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/x", func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		middleware.RequireRole(users, "super_admin")(c)
		if !c.IsAborted() {
			c.Status(http.StatusOK)
		}
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRequireRole_DeniesMissingRole(t *testing.T) {
	users := &fakeAuthUsers{roles: []string{"client"}}

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/x", func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		middleware.RequireRole(users, "super_admin")(c)
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestRequireRole_DeniesUnauthenticated(t *testing.T) {
	users := &fakeAuthUsers{}

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/x", func(c *gin.Context) {
		// No user_id set → simulate unauthenticated request.
		middleware.RequireRole(users, "admin")(c)
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}
