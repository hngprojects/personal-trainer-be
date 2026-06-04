package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
)

func (s *routerImpl) HandleCreateDevToken(c *gin.Context, params api.HandleCreateDevTokenParams) {
	if s.dev == nil {
		s.logger.Warn("HandleCreateDevToken: dev handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.dev.HandleCreateDevToken(c, params)
}
