package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
)

const (
	codeExpiry         = 15 * time.Minute
	refreshTokenExpiry = 7 * 24 * time.Hour
	accessTokenTTL     = 10 * time.Minute
)

type LocalHandler struct {
	users     UserRepository
	sessions  SessionRepository
	codes     VerificationCodeRepository
	mailer    email.Mailer
	log       *slog.Logger
	limiter   *verifyRateLimiter
	otpSecret string
}

func NewLocalHandler(
	users UserRepository,
	sessions SessionRepository,
	codes VerificationCodeRepository,
	mailer email.Mailer,
	log *slog.Logger,
	otpSecret string,
) *LocalHandler {
	return &LocalHandler{
		users:     users,
		sessions:  sessions,
		codes:     codes,
		mailer:    mailer,
		log:       log,
		limiter:   newVerifyRateLimiter(),
		otpSecret: otpSecret,
	}
}

// Close stops the background rate-limiter cleanup goroutine.
func (h *LocalHandler) Close() {
	h.limiter.Stop()
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

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	if !common.IsValidEmail(req.Email) {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "email", Message: "invalid email format"},
		}))
		return
	}

	_, err := h.users.FindByEmailAndProvider(c.Request.Context(), req.Email, "local")
	if err != nil && !errors.Is(err, ErrNotFound) {
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	if errors.Is(err, ErrNotFound) {
		if _, err = h.users.CreateEmailUser(c.Request.Context(), req.Email); err != nil {
			if errors.Is(err, ErrEmailExists) {
				c.JSON(http.StatusConflict, api.NewError("email already registered", api.CodeConflict))
				return
			}
			c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
			return
		}
	}

	// Clear any previous codes for this email before creating a new one
	_ = h.codes.DeleteByEmail(c.Request.Context(), req.Email)

	code, err := generateVerificationCode()
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	if err := h.codes.Create(c.Request.Context(), req.Email, h.hashOTP(code), time.Now().Add(codeExpiry)); err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	subject := "Your verification code"
	body := fmt.Sprintf("Your verification code is: %s\n\nThis code expires in %d minutes.", code, int(codeExpiry.Minutes()))
	if err := h.mailer.Send(req.Email, subject, body); err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to send verification email", api.CodeServerError))
		return
	}

	h.limiter.reset(req.Email)
	c.JSON(http.StatusCreated, api.NewSuccess("Verification code sent to your email", api.CodeCreated, nil))
}

func (h *LocalHandler) VerifyEmail(c *gin.Context) {
	var req verifyEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

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

	if !h.limiter.allow(req.Email) {
		c.JSON(http.StatusTooManyRequests, api.NewError("too many attempts, please request a new code", api.CodeTooManyRequests))
		return
	}

	_, err := h.codes.ConsumeByEmailAndCode(c.Request.Context(), req.Email, h.hashOTP(req.Code))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusBadRequest, api.NewError("invalid or expired verification code", api.CodeBadRequest))
			return
		}
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

	_, err = h.sessions.Create(c.Request.Context(), user.ID, refreshToken, time.Now().Add(refreshTokenExpiry))
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	h.limiter.reset(req.Email)
	h.log.Info("user verified and logged in", "user_id", user.ID.String())

	data := map[string]interface{}{
		"user": map[string]interface{}{
			"id":               user.ID.String(),
			"email":            user.Email,
			"name":             user.Name,
			"user_type":        "client",
			"profile_complete": user.Name != "",
		},
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"expires_in":    int(accessTokenTTL / time.Second),
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

func (h *LocalHandler) hashOTP(code string) string {
	mac := hmac.New(sha256.New, []byte(h.otpSecret))
	mac.Write([]byte(code))
	return hex.EncodeToString(mac.Sum(nil))
}
