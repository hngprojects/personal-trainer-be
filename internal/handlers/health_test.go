package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/handlers"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestHealthCheck_Status(t *testing.T) {
	h := handlers.NewHealthHandler()

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/health", h.Check)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHealthCheck_ResponseBody(t *testing.T) {
	h := handlers.NewHealthHandler()

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/health", h.Check)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("expected status field to be \"ok\", got %v", body["status"])
	}
	if _, ok := body["time"]; !ok {
		t.Error("expected time field in response body")
	}
}

func TestHealthCheck_ContentType(t *testing.T) {
	h := handlers.NewHealthHandler()

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/health", h.Check)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("expected Content-Type application/json; charset=utf-8, got %q", ct)
	}
}

func TestHealthRoot_Status(t *testing.T) {
	h := handlers.NewHealthHandler()

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/", h.Root)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHealthRoot_ResponseBody(t *testing.T) {
	h := handlers.NewHealthHandler()

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/", h.Root)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	want := "personal-trainer-be is running"
	if body["message"] != want {
		t.Errorf("expected message %q, got %q", want, body["message"])
	}
}
