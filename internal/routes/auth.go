package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
)

func (s *routerImpl) HandleGoogleLogin(c *gin.Context) {
	if s.google == nil {
		s.logger.Warn("HandleGoogleLogin: google handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.google.HandleGoogleLogin(c)
}

func (s *routerImpl) HandleGoogleCallback(c *gin.Context, params api.HandleGoogleCallbackParams) {
	if s.google == nil {
		s.logger.Warn("HandleGoogleCallback: google handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.google.HandleGoogleCallback(c, params.State, params.Code)
}

func (s *routerImpl) HandleGoogleMobileSignIn(c *gin.Context) {
	if s.googleMobile == nil {
		s.logger.Warn("HandleGoogleMobileSignIn: google mobile handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.googleMobile.SignIn(c)
}

func (s *routerImpl) HandleLogout(c *gin.Context) {
	if s.logout == nil {
		s.logger.Warn("HandleLogout: logout handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.logout.HandleLogout(c)
}

func (s *routerImpl) HandleRefresh(c *gin.Context) {
	if s.refresh == nil {
		s.logger.Warn("HandleRefresh: refresh handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.refresh.HandleRefresh(c)
}

func (s *routerImpl) HandleVerifyEmail(c *gin.Context) {
	if s.local == nil {
		s.logger.Warn("HandleVerifyEmail: local auth handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.local.VerifyEmail(c)
}

func (s *routerImpl) HandleForgotPassword(c *gin.Context) {
	if s.passwordReset == nil {
		s.logger.Warn("HandleForgotPassword: password reset handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.passwordReset.HandleForgotPassword(c)
}

func (s *routerImpl) HandleResetPassword(c *gin.Context) {
	if s.passwordReset == nil {
		s.logger.Warn("HandleResetPassword: password reset handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.passwordReset.HandleResetPassword(c)
}

func (s *routerImpl) HandleSetPassword(c *gin.Context) {
	if s.accountSetup == nil {
		s.logger.Warn("HandleSetPassword: account setup handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.accountSetup.HandleSetPassword(c)
}

func (s *routerImpl) HandleAdminLogin(c *gin.Context) {
	if s.adminLogin == nil {
		s.logger.Warn("HandleAdminLogin: admin login handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.adminLogin.Login(c)
}

func (s *routerImpl) HandleLocalAuth(c *gin.Context) {
	if s.local == nil {
		s.logger.Warn("HandleLocalAuth: local auth handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.local.SignIn(c)
}

func (s *routerImpl) HandleRegister(c *gin.Context) {
	if s.local == nil {
		s.logger.Warn("HandleRegister: local auth handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.local.Register(c)
}
