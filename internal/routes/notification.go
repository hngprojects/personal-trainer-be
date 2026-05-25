package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
)

func (s *routerImpl) HandleSendNotification(c *gin.Context) {
	if s.notificationHandler == nil {
		s.logger.Warn("HandleSendNotification: notification handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.notificationHandler.SendNotificationToUser(c)
}

func (s *routerImpl) HandleGetUserNotifications(c *gin.Context) {
	if s.notificationHandler == nil {
		s.logger.Warn("HandleSendNotification: notification handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.notificationHandler.GetUserNotifications(c)
}
