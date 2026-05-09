package routes

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
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
	if s.users == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	var req struct {
		Email    string `json:"email"    binding:"required,email"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid input", api.CodeBadRequest))
		return
	}

	user, err := s.users.FindByEmail(c.Request.Context(), req.Email)
	if err != nil || !user.PasswordHash.Valid {
		c.JSON(http.StatusUnauthorized, api.NewError("invalid email or password", api.CodeUnauthorized))
		return
	}
	if err := auth.CheckPassword(user.PasswordHash.String, req.Password); err != nil {
		c.JSON(http.StatusUnauthorized, api.NewError("invalid email or password", api.CodeUnauthorized))
		return
	}

	_ = s.users.UpdateLastLogin(c.Request.Context(), user.ID)
	access, _ := auth.GenerateJWTToken(user.ID.String(), auth.AccessToken)
	refresh, _ := auth.GenerateJWTToken(user.ID.String(), auth.RefreshToken)

	c.JSON(http.StatusOK, api.NewSuccessResponse("login successful", api.CodeOK, map[string]interface{}{
		"user": map[string]interface{}{
			"id":    user.ID,
			"email": user.Email,
			"name":  user.Name,
		},
		"access_token":  access,
		"refresh_token": refresh,
		"expires_in":    int(10 * time.Minute / time.Second),
	}, nil))
}
