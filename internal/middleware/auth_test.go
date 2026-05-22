package middleware_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/internal/middleware"
	appredis "github.com/hngprojects/personal-trainer-be/pkg/redis"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

func init() {
	gin.SetMode(gin.TestMode)
	if err := os.Setenv("JWT_SECRET", "test-secret"); err != nil {
		panic(err)
	}
}

type fakeRedis struct {
	blocked map[string]bool
	err     error
}

func (f *fakeRedis) Set(_ context.Context, _ string, _ interface{}, _ time.Duration) error {
	return f.err
}

func (f *fakeRedis) Exists(_ context.Context, key string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.blocked[key], nil
}

func makeToken(t *testing.T, userID, jti, tokenType, secret string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":  userID,
		"exp":  time.Now().Add(10 * time.Minute).Unix(),
		"iat":  time.Now().Unix(),
		"iss":  "api.fitcall",
		"type": tokenType,
		"jti":  jti,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return signed
}

func setupRouter(redis appredis.RedisClient, log *slog.Logger) (*gin.Engine, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.Use(middleware.AuthMiddleware(redis, log))
	r.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return r, w
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	r, w := setupRouter(&fakeRedis{}, testLogger)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_InvalidTokenFormat(t *testing.T) {
	r, w := setupRouter(&fakeRedis{}, testLogger)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "InvalidFormat")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_InvalidSignature(t *testing.T) {
	jti := uuid.NewString()
	userID := uuid.NewString()
	token := makeToken(t, userID, jti, "access", "wrong-secret")

	r, w := setupRouter(&fakeRedis{}, testLogger)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_BlocklistedToken(t *testing.T) {
	jti := uuid.NewString()
	userID := uuid.NewString()
	token := makeToken(t, userID, jti, "access", "test-secret")

	r, w := setupRouter(&fakeRedis{blocked: map[string]bool{"blocklist:" + jti: true}}, testLogger)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for blocklisted token, got %d", w.Code)
	}
}

func TestAuthMiddleware_ValidToken_SetsContext(t *testing.T) {
	jti := uuid.NewString()
	userID := uuid.New()
	token := makeToken(t, userID.String(), jti, "access", "test-secret")

	var gotUserID uuid.UUID
	var gotJTI string

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.Use(middleware.AuthMiddleware(&fakeRedis{}, testLogger))
	r.GET("/protected", func(c *gin.Context) {
		gotUserID, _ = c.MustGet("user_id").(uuid.UUID)
		gotJTI, _ = c.MustGet(string(common.ContextKeyJTI)).(string)
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if gotUserID != userID {
		t.Errorf("expected userID %s, got %s", userID, gotUserID)
	}
	if gotJTI != jti {
		t.Errorf("expected jti %s, got %s", jti, gotJTI)
	}
}

func TestAuthMiddleware_RedisError(t *testing.T) {
	jti := uuid.NewString()
	userID := uuid.NewString()
	token := makeToken(t, userID, jti, "access", "test-secret")

	r, w := setupRouter(&fakeRedis{err: fmt.Errorf("redis down")}, testLogger)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on redis error, got %d", w.Code)
	}
}
