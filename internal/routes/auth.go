package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
)

func (s *routerImpl) HandleGoogleLogin(c *gin.Context) {
	if s.google == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.google.HandleGoogleLogin(c)
}

func (s *routerImpl) HandleGoogleCallback(c *gin.Context, params api.HandleGoogleCallbackParams) {
	if s.google == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.google.HandleGoogleCallback(c, params.State, params.Code)
}

func (s *routerImpl) HandleLocalAuth(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, api.NewError("not implemented", api.CodeNotImplemented))
}
