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
	users           UserRepository
	sessions        SessionRepository
	codes           VerificationCodeRepository
	mailer          email.Mailer
	log             *slog.Logger
	verifyLimiter   *verifyRateLimiter
	registerLimiter *verifyRateLimiter
	otpSecret       string
}

func NewLocalHandler(
	users UserRepository,
	sessions SessionRepository,
	codes VerificationCodeRepository,
	mailer email.Mailer,
	log *slog.Logger,
	otpSecret string,
) *LocalHandler {
	if otpSecret == "" {
		log.Warn("OTP_SECRET is not set — OTP hashes have no secret protection; set OTP_SECRET in production")
	}
	return &LocalHandler{
		users:           users,
		sessions:        sessions,
		codes:           codes,
		mailer:          mailer,
		log:             log,
		verifyLimiter:   newRateLimiter(maxVerifyAttempts),
		registerLimiter: newRateLimiter(maxRegisterAttempts),
		otpSecret:       otpSecret,
	}
}

// Close stops the background rate-limiter cleanup goroutines.
func (h *LocalHandler) Close() {
	h.verifyLimiter.Stop()
	h.registerLimiter.Stop()
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

	if !h.registerLimiter.allow(req.Email) {
		c.JSON(http.StatusTooManyRequests, api.NewError("too many requests, please try again later", api.CodeTooManyRequests))
		return
	}

	_, err := h.users.FindByEmailAndProvider(c.Request.Context(), req.Email, "local")
	if err != nil && !errors.Is(err, ErrNotFound) {
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	if errors.Is(err, ErrNotFound) {
		if _, err = h.users.CreateEmailUser(c.Request.Context(), req.Email); err != nil {
			c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
			return
		}
	}

	// Clear any previous codes — abort if this fails to keep one-code-at-a-time semantics
	if err := h.codes.DeleteByEmail(c.Request.Context(), req.Email); err != nil {
		h.log.Error("failed to delete previous verification codes", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

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

	h.verifyLimiter.reset(req.Email)
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

	req.Code = strings.TrimSpace(req.Code)
	if req.Code == "" {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "code", Message: "code is required"},
		}))
		return
	}
	if len(req.Code) != 6 || !isDigits(req.Code) {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "code", Message: "code must be a 6-digit number"},
		}))
		return
	}

	if !h.verifyLimiter.allow(req.Email) {
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

	h.verifyLimiter.reset(req.Email)
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

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (h *LocalHandler) hashOTP(code string) string {
	mac := hmac.New(sha256.New, []byte(h.otpSecret))
	mac.Write([]byte(code))
	return hex.EncodeToString(mac.Sum(nil))
}
