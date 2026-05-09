package root

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
)

type RootHandler struct {
	log *slog.Logger
}

func NewRootHandler(log *slog.Logger) *RootHandler {
	return &RootHandler{log: log}
}

func (h *RootHandler) Root(c *gin.Context) {
	h.log.Debug("root endpoint called")
	c.JSON(http.StatusOK, api.NewSuccess("Personal Trainer API is running", api.CodeOK, nil))
}
