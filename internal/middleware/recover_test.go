package middleware_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/middleware"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func init() {
	gin.SetMode(gin.TestMode)
}

func TestRecover_CatchesPanic(t *testing.T) {
	log := discardLogger()

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.Use(middleware.Recover(log))
	r.GET("/panic", func(c *gin.Context) {
		panic("simulated panic")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestRecover_ReturnsJSONError(t *testing.T) {
	log := discardLogger()

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.Use(middleware.Recover(log))
	r.GET("/panic", func(c *gin.Context) {
		panic("simulated panic")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	r.ServeHTTP(w, req)

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("expected JSON response body, got decode error: %v", err)
	}
	if body["error"] != "internal server error" {
		t.Errorf("expected error message %q, got %q", "internal server error", body["error"])
	}
}

func TestRecover_DoesNotAffectNormalRequests(t *testing.T) {
	log := discardLogger()

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.Use(middleware.Recover(log))
	r.GET("/ok", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for normal request, got %d", w.Code)
	}
}
