package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/db"
	"github.com/hngprojects/personal-trainer-be/internal/service"
)

type AuthHandler struct {
	auth       *service.AuthService
	oauthCfg   *oauth2.Config
	queries    *db.Queries
	sessionTTL time.Duration
}

func NewAuthHandler(auth *service.AuthService, cfg *config.Config, queries *db.Queries) *AuthHandler {
	return &AuthHandler{
		auth:       auth,
		queries:    queries,
		sessionTTL: cfg.SessionTTL,
		oauthCfg: &oauth2.Config{
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
			RedirectURL:  cfg.GoogleRedirectURL,
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     google.Endpoint,
		},
	}
}

// POST /auth/register
func (h *AuthHandler) InitiateSignUp(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("VALIDATION_FAILED", "valid email is required"))
		return
	}

	if err := h.auth.InitiateSignUp(c.Request.Context(), req.Email); err != nil {
		if errors.Is(err, service.ErrEmailAlreadyExists) {
			c.JSON(http.StatusConflict, errorResponse("EMAIL_EXISTS", "email already registered"))
			return
		}
		c.JSON(http.StatusInternalServerError, errorResponse("INTERNAL_ERROR", "something went wrong"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "verification code sent"})
}

// POST /auth/register/verify
func (h *AuthHandler) VerifyCode(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
		Code  string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("VALIDATION_FAILED", "email and code are required"))
		return
	}

	if err := h.auth.VerifyCode(c.Request.Context(), req.Email, req.Code); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_CODE", "invalid or expired verification code"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "code verified"})
}

// POST /auth/register/complete
func (h *AuthHandler) CompleteSignUp(c *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Name     string `json:"name" binding:"required"`
		Code     string `json:"code" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("VALIDATION_FAILED", "email, name, code, and password are required"))
		return
	}

	session, err := h.auth.CompleteSignUp(c.Request.Context(), req.Email, req.Name, req.Code, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCode):
			c.JSON(http.StatusBadRequest, errorResponse("INVALID_CODE", "invalid or expired verification code"))
		case errors.Is(err, service.ErrWeakPassword):
			c.JSON(http.StatusBadRequest, errorResponse("WEAK_PASSWORD", err.Error()))
		default:
			c.JSON(http.StatusInternalServerError, errorResponse("INTERNAL_ERROR", "something went wrong"))
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status": "account created",
		"data": gin.H{
			"session_id": session.ID,
			"expires_at": session.ExpiresAt,
		},
	})
}

// POST /auth/login
func (h *AuthHandler) SignIn(c *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("VALIDATION_FAILED", "email and password are required"))
		return
	}

	session, user, err := h.auth.SignIn(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			c.JSON(http.StatusUnauthorized, errorResponse("INVALID_CREDENTIALS", "invalid email or password"))
		case errors.Is(err, service.ErrAccountNotActive):
			c.JSON(http.StatusForbidden, errorResponse("ACCOUNT_INACTIVE", "account is not active"))
		default:
			c.JSON(http.StatusInternalServerError, errorResponse("INTERNAL_ERROR", "something went wrong"))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "logged in",
		"data": gin.H{
			"session_id": session.ID,
			"expires_at": session.ExpiresAt,
			"user": gin.H{
				"id":    user.ID,
				"email": user.Email,
				"name":  user.Name,
			},
		},
	})
}

func errorResponse(code, message string) gin.H {
	return gin.H{
		"error": gin.H{
			"code":    code,
			"message": message,
		},
	}
}
