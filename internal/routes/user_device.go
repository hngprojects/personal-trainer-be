package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
)

func (s *routerImpl) HandleRegisterDevice(c *gin.Context) {
	if s.userDeviceHandler == nil {
		s.logger.Warn("HandleRegisterDevice: user device handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.userDeviceHandler.HandleRegisterDevice(c)
}
