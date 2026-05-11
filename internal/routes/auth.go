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
	if s.local == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.local.SignIn(c)
}

func (s *routerImpl) HandleLogout(c *gin.Context) {
	if s.logout == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.logout.HandleLogout(c)
}

func (s *routerImpl) HandleRegister(c *gin.Context) {
	if s.local == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.local.Register(c)
}

func (s *routerImpl) HandleVerifyEmail(c *gin.Context) {
	if s.local == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.local.VerifyEmail(c)
}

func (s *routerImpl) HandleForgotPassword(c *gin.Context) {
	if s.passwordReset == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.passwordReset.HandleForgotPassword(c)
}

func (s *routerImpl) HandleResetPassword(c *gin.Context) {
	if s.passwordReset == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.passwordReset.HandleResetPassword(c)
}
