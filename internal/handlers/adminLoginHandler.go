package handlers

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/common"
)

type AdminLoginHandler struct {
	service auth.AdminAuthService
	log     *slog.Logger
}

func NewAdminLogin(service auth.AdminAuthService, log *slog.Logger) *AdminLoginHandler {
	return &AdminLoginHandler{service: service, log: log}
}

func (h *AdminLoginHandler) Login(c *gin.Context) {
	var request api.HandleAdminLoginJSONBody
	if err := c.ShouldBindJSON(&request); err != nil {
		h.log.Error("error binding request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request", api.CodeBadRequest))
		return
	}
	email := string(request.Email)
	password := request.Password
	var fieldErrors []api.FieldError
	if !common.IsNotEmpty(email) || !common.IsValidEmail(email) {
		fieldErrors = append(fieldErrors, api.FieldError{Field: "email", Message: "Please provide a valid email address"})
	}
	if !common.IsNotEmpty(password) {
		fieldErrors = append(fieldErrors, api.FieldError{Field: "password", Message: "Password is required"})
	}
	if len(fieldErrors) > 0 {
		h.log.Warn("AdminLogin: validation failed", "field_errors", len(fieldErrors))
		c.JSON(http.StatusBadRequest, api.NewValidationError(fieldErrors))
		return
	}
	result, err := h.service.Login(c.Request.Context(), email, password)
	if err != nil {
		h.log.Warn("Admin login: service returned error", "err", err)
		c.JSON(http.StatusUnauthorized, api.ErrorResponse{Code: api.CodeUnauthorized, Message: "invalid email or password", Status: "error"})
		return
	}
	h.log.Info("user successfully logged in", "email", request.Email)
	c.JSON(http.StatusOK, result)
}
