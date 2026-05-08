package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
)

func (s *routerImpl) HandleAddWaitlist(c *gin.Context) {
	if s.waitlist == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.waitlist.HandleAddWaitlist(c)
}

func (s *routerImpl) HandleGetWaitlist(c *gin.Context, params api.HandleGetWaitlistParams) {
	if s.waitlist == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.waitlist.HandleGetWaitlist(c, params)
}
