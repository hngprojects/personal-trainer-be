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

func (s *routerImpl) HandleGoogleMobileSignIn(c *gin.Context) {
	if s.googleMobile == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.googleMobile.SignIn(c)
}

func (s *routerImpl) HandleLogout(c *gin.Context) {
	if s.logout == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.logout.HandleLogout(c)
}

func (s *routerImpl) HandleRefresh(c *gin.Context) {
	if s.refresh == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.refresh.HandleRefresh(c)
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

func (s *routerImpl) HandleAdminLogin(c *gin.Context) {
	if s.adminLogin == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.adminLogin.Login(c)
}
