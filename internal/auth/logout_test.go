package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	appredis "github.com/hngprojects/personal-trainer-be/pkg/redis"
)

func init() {
	gin.SetMode(gin.TestMode)
	_ = os.Setenv("JWT_SECRET", "test-secret") // init() context — t.Setenv unavailable
}

type fakeRedis struct {
	stored map[string]bool
	err    error
}

func (f *fakeRedis) Set(_ context.Context, key string, _ interface{}, _ time.Duration) error {
	if f.err != nil {
		return f.err
	}
	f.stored[key] = true
	return nil
}

func (f *fakeRedis) Exists(_ context.Context, key string) (bool, error) {
	return f.stored[key], f.err
}

func makeRefreshToken(t *testing.T, userID, jti string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":  userID,
		"exp":  time.Now().Add(7 * 24 * time.Hour).Unix(),
		"iat":  time.Now().Unix(),
		"iss":  "api.fitcall",
		"type": "refresh",
		"jti":  jti,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return signed
}

func makeAccessToken(t *testing.T, userID, jti string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":  userID,
		"exp":  time.Now().Add(10 * time.Minute).Unix(),
		"iat":  time.Now().Unix(),
		"iss":  "api.fitcall",
		"type": "access",
		"jti":  jti,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return signed
}

func logoutRouter(redis appredis.RedisClient) (*gin.Engine, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := auth.NewLogoutHandler(redis, log)
	r.POST("/auth/logout", h.HandleLogout)
	return r, w
}

func TestHandleLogout_MissingRefreshToken(t *testing.T) {
	r, w := logoutRouter(&fakeRedis{stored: map[string]bool{}})

	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/logout", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleLogout_InvalidRefreshToken(t *testing.T) {
	r, w := logoutRouter(&fakeRedis{stored: map[string]bool{}})

	body, _ := json.Marshal(map[string]string{"refresh_token": "not-a-valid-token"})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/logout", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestHandleLogout_WrongTokenType(t *testing.T) {
	r, w := logoutRouter(&fakeRedis{stored: map[string]bool{}})

	accessToken := makeAccessToken(t, uuid.NewString(), uuid.NewString())
	body, _ := json.Marshal(map[string]string{"refresh_token": accessToken})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/logout", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for access token used as refresh token, got %d", w.Code)
	}
}

func TestHandleLogout_RedisError(t *testing.T) {
	r, w := logoutRouter(&fakeRedis{err: fmt.Errorf("redis down"), stored: map[string]bool{}})

	refreshToken := makeRefreshToken(t, uuid.NewString(), uuid.NewString())
	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/logout", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on redis error, got %d", w.Code)
	}
}

func TestHandleLogout_Success(t *testing.T) {
	fake := &fakeRedis{stored: map[string]bool{}}
	r, w := logoutRouter(fake)

	jti := uuid.NewString()
	refreshToken := makeRefreshToken(t, uuid.NewString(), jti)
	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/logout", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !fake.stored["blocklist:"+jti] {
		t.Errorf("expected jti to be blocklisted in redis")
	}
}
