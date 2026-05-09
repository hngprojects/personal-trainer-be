package auth

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
)

type LoginHandler struct {
	users    UserRepository
	sessions SessionRepository
	roles    RoleRepository
	log      *slog.Logger
}

func NewLoginHandler(
	users UserRepository,
	sessions SessionRepository,
	roles RoleRepository,
	log *slog.Logger,
) *LoginHandler {
	return &LoginHandler{
		users:    users,
		sessions: sessions,
		roles:    roles,
		log:      log,
	}
}

func (h *LoginHandler) Login(c *gin.Context) {
	var req api.HandleLocalAuthJSONRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	emailAddr := strings.ToLower(strings.TrimSpace(string(req.Email)))
	password := req.Password

	var fieldErrs []api.FieldError
	if len(emailAddr) > 255 || !common.IsValidEmail(emailAddr) {
		fieldErrs = append(fieldErrs, api.FieldError{Field: "email", Message: "invalid email format"})
	}
	if password == "" {
		fieldErrs = append(fieldErrs, api.FieldError{Field: "password", Message: "password is required"})
	}
	if len(fieldErrs) > 0 {
		c.JSON(http.StatusBadRequest, api.NewValidationError(fieldErrs))
		return
	}

	// Generic message used for every authentication failure path so a probe
	// cannot distinguish "no such email", "wrong password", "deactivated",
	// or "OAuth-only account" from each other.
	const genericAuthFail = "invalid email or password"

	user, err := h.users.FindByEmailAndProvider(c.Request.Context(), emailAddr, providerLocal)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusUnauthorized, api.NewError(genericAuthFail, api.CodeUnauthorized))
			return
		}
		h.log.Error("failed to look up user for login", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	if !user.IsActive || !user.Password.Valid || user.Password.String == "" {
		c.JSON(http.StatusUnauthorized, api.NewError(genericAuthFail, api.CodeUnauthorized))
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password.String), []byte(password)); err != nil {
		c.JSON(http.StatusUnauthorized, api.NewError(genericAuthFail, api.CodeUnauthorized))
		return
	}

	// Highest-privilege role wins for the response's user_type field.
	userType := api.AuthUserUserTypeClient
	if isAdmin, err := h.roles.UserHasRole(c.Request.Context(), user.ID, "admin"); err != nil {
		h.log.Warn("failed to check admin role — defaulting to client", "err", err, "user_id", user.ID.String())
	} else if isAdmin {
		userType = api.AuthUserUserTypeAdmin
	} else if isTrainer, err := h.roles.UserHasRole(c.Request.Context(), user.ID, "trainer"); err != nil {
		h.log.Warn("failed to check trainer role — defaulting to client", "err", err, "user_id", user.ID.String())
	} else if isTrainer {
		userType = api.AuthUserUserTypeTrainer
	}

	userIDStr := user.ID.String()
	accessToken, err := GenerateJWTToken(userIDStr, AccessToken)
	if err != nil {
		h.log.Error("failed to generate access token", "user_id", userIDStr, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	refreshToken, err := GenerateJWTToken(userIDStr, RefreshToken)
	if err != nil {
		h.log.Error("failed to generate refresh token", "user_id", userIDStr, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	if _, err := h.sessions.Create(c.Request.Context(), user.ID, refreshToken, time.Now().Add(refreshTokenExpiry)); err != nil {
		h.log.Error("failed to create session", "user_id", userIDStr, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	h.log.Info("user logged in", "user_id", userIDStr, "user_type", string(userType))

	data := api.LocalAuthData{
		User: api.AuthUser{
			Id:              user.ID,
			Email:           user.Email,
			Name:            user.Name,
			UserType:        userType,
			ProfileComplete: user.Name != "",
		},
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int(accessTokenTTL / time.Second),
	}
	c.JSON(http.StatusOK, api.NewSuccess("Login successful", api.CodeOK, data))
}
