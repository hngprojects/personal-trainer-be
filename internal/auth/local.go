package auth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
)

type LocalHandler struct {
	users    UserRepository
	sessions SessionRepository
	codes    VerificationCodeRepository
	mailer   email.Mailer
	log      *slog.Logger
}

func NewLocalHandler(
	users UserRepository,
	sessions SessionRepository,
	codes VerificationCodeRepository,
	mailer email.Mailer,
	log *slog.Logger,
) *LocalHandler {
	return &LocalHandler{
		users:    users,
		sessions: sessions,
		codes:    codes,
		mailer:   mailer,
		log:      log,
	}
}

type registerRequest struct {
	Email string `json:"email"`
}

type verifyEmailRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func (h *LocalHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	if !common.IsValidEmail(req.Email) {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "email", Message: "invalid email format"},
		}))
		return
	}

	_, err := h.users.FindByEmailAndProvider(c.Request.Context(), req.Email, "local")
	if err == nil {
		c.JSON(http.StatusConflict, api.NewError("email already registered", api.CodeConflict))
		return
	}
	if !errors.Is(err, ErrNotFound) {
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	_, err = h.users.CreateEmailUser(c.Request.Context(), req.Email)
	if err != nil {
		if errors.Is(err, ErrEmailExists) {
			c.JSON(http.StatusConflict, api.NewError("email already registered", api.CodeConflict))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	code, err := generateVerificationCode()
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	if err := h.codes.Create(c.Request.Context(), req.Email, code, time.Now().Add(15*time.Minute)); err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	subject := "Your verification code"
	body := fmt.Sprintf("Your verification code is: %s\n\nThis code expires in 15 minutes.", code)
	if err := h.mailer.Send(req.Email, subject, body); err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to send verification email", api.CodeServerError))
		return
	}

	c.JSON(http.StatusCreated, api.NewSuccess("Verification code sent to your email", api.CodeCreated, nil))
}

func (h *LocalHandler) VerifyEmail(c *gin.Context) {
	var req verifyEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	if !common.IsValidEmail(req.Email) {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "email", Message: "invalid email format"},
		}))
		return
	}

	if req.Code == "" {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "code", Message: "code is required"},
		}))
		return
	}

	_, err := h.codes.GetByEmailAndCode(c.Request.Context(), req.Email, req.Code)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusBadRequest, api.NewError("invalid or expired verification code", api.CodeBadRequest))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	if err := h.codes.DeleteByEmail(c.Request.Context(), req.Email); err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	user, err := h.users.MarkVerified(c.Request.Context(), req.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	userIDStr := user.ID.String()
	accessToken, err := GenerateJWTToken(userIDStr, AccessToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	refreshToken, err := GenerateJWTToken(userIDStr, RefreshToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	_, err = h.sessions.Create(c.Request.Context(), user.ID, refreshToken, time.Now().Add(7*24*time.Hour))
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	h.log.Info("user verified and logged in", "email", user.Email, "user_id", user.ID.String())

	data := map[string]interface{}{
		"user": map[string]interface{}{
			"id":               user.ID.String(),
			"email":            user.Email,
			"name":             user.Name,
			"user_type":        "client",
			"profile_complete": false,
		},
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"expires_in":    int(10 * time.Minute / time.Second),
	}
	c.JSON(http.StatusOK, api.NewSuccess("Email verified successfully", api.CodeOK, data))
}

func generateVerificationCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
