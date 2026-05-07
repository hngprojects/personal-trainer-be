// health/health.go
package health

import (
	"net/http"
	"time"

	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
)

type HealthHandler struct {
	log *slog.Logger
}

func NewHealthHandler(log *slog.Logger) *HealthHandler {
	return &HealthHandler{log: log}
}

func (h *HealthHandler) Check(c *gin.Context) {
	h.log.Debug("health check called")
	c.JSON(http.StatusOK, api.NewSuccess("Service is healthy", api.CodeOK, map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}))
}
