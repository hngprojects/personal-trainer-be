package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
)

func (s *routerImpl) HandleContactUs(c *gin.Context) {
	if s.contact == nil {
		s.logger.Warn("HandleContactUs: contact handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.contact.HandleContactUs(c)
}
