package root

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRootHandler_Root(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		wantStatus int
	}{
		{
			name:       "returns 200 OK",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			handler := NewRootHandler(log)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

			handler.Root(c)

			if w.Code != tt.wantStatus {
				t.Errorf("Root() status = %v, want %v", w.Code, tt.wantStatus)
			}
		})
	}
}
