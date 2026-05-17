package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/api"
)

// CreateZoomMeeting handles POST /integrations/zoom/create-meeting
func (s *routerImpl) CreateZoomMeeting(c *gin.Context) {
	if s.zoomIntegration == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("zoom integration not configured", api.CodeServerError))
		return
	}
	s.zoomIntegration.CreateZoomMeeting(c)
}
