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
	"github.com/hngprojects/personal-trainer-be/pkg/ratelimit"
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
	localAuth       LocalAuthRepository
	mailer          email.Mailer
	log             *slog.Logger
	verifyLimiter   ratelimit.RateLimiter
	registerLimiter ratelimit.RateLimiter
	otpSecret       string
}

func NewLocalHandler(
	users UserRepository,
	sessions SessionRepository,
	codes VerificationCodeRepository,
	localAuth LocalAuthRepository,
	mailer email.Mailer,
	log *slog.Logger,
	otpSecret string,
	verifyLimiter ratelimit.RateLimiter,
	registerLimiter ratelimit.RateLimiter,
) *LocalHandler {
	if otpSecret == "" {
		log.Warn("OTP_SECRET is not set — OTP hashes have no secret protection; set OTP_SECRET in production")
	}
	return &LocalHandler{
		users:           users,
		sessions:        sessions,
		codes:           codes,
		localAuth:       localAuth,
		mailer:          mailer,
		log:             log,
		verifyLimiter:   verifyLimiter,
		registerLimiter: registerLimiter,
		otpSecret:       otpSecret,
	}
}

func (h *LocalHandler) Register(c *gin.Context) {
	var req api.HandleRegisterJSONRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	emailAddr := strings.ToLower(strings.TrimSpace(string(req.Email)))

	if len(emailAddr) > 255 {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "email", Message: "email must not exceed 255 characters"},
		}))
		return
	}

	if !common.IsValidEmail(emailAddr) {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "email", Message: "invalid email format"},
		}))
		return
	}

	if allowed, err := h.registerLimiter.Allow(c.Request.Context(), emailAddr); err != nil {
		h.log.Warn("register rate limiter error — failing open", "err", err)
	} else if !allowed {
		c.JSON(http.StatusTooManyRequests, api.NewError("too many requests, please try again later", api.CodeTooManyRequests))
		return
	}

	_, err := h.users.FindByEmailAndProvider(c.Request.Context(), emailAddr, providerLocal)
	if err != nil && !errors.Is(err, ErrNotFound) {
		h.log.Error("failed to find user by email", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	if errors.Is(err, ErrNotFound) {
		if _, err = h.users.CreateEmailUser(c.Request.Context(), emailAddr); err != nil && !errors.Is(err, ErrEmailExists) {
			h.log.Error("failed to create email user", "err", err)
			c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
			return
		}
	}

	// Clear any previous codes — abort if this fails to keep one-code-at-a-time semantics
	if err := h.codes.DeleteByEmail(c.Request.Context(), emailAddr); err != nil {
		h.log.Error("failed to delete previous verification codes", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	code, err := generateVerificationCode()
	if err != nil {
		h.log.Error("failed to generate verification code", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	if err := h.codes.Create(c.Request.Context(), emailAddr, h.hashOTP(code), time.Now().Add(codeExpiry)); err != nil {
		h.log.Error("failed to store verification code", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	if err := h.mailer.SendVerificationCode(emailAddr, code, int(codeExpiry.Minutes())); err != nil {
		h.log.Error("failed to send verification email", "email", emailAddr, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	if err := h.verifyLimiter.Reset(c.Request.Context(), emailAddr); err != nil {
		h.log.Warn("failed to reset verify limiter", "err", err)
	}
	c.JSON(http.StatusCreated, api.NewSuccess("Verification code sent to your email", api.CodeCreated, nil))
}

func (h *LocalHandler) VerifyEmail(c *gin.Context) {
	var req api.HandleVerifyEmailJSONRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	emailAddr := strings.ToLower(strings.TrimSpace(string(req.Email)))
	code := strings.TrimSpace(req.Code)

	if len(emailAddr) > 255 {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "email", Message: "email must not exceed 255 characters"},
		}))
		return
	}

	if !common.IsValidEmail(emailAddr) {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "email", Message: "invalid email format"},
		}))
		return
	}

	if code == "" {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "code", Message: "code is required"},
		}))
		return
	}
	if len(code) != 6 || !isDigits(code) {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "code", Message: "code must be a 6-digit number"},
		}))
		return
	}

	if allowed, err := h.verifyLimiter.Allow(c.Request.Context(), emailAddr); err != nil {
		h.log.Warn("verify rate limiter error — failing open", "err", err)
	} else if !allowed {
		c.JSON(http.StatusTooManyRequests, api.NewError("too many attempts, please request a new code", api.CodeTooManyRequests))
		return
	}

	user, err := h.localAuth.ConsumeAndMarkVerified(c.Request.Context(), emailAddr, h.hashOTP(code))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusBadRequest, api.NewError("invalid or expired verification code", api.CodeBadRequest))
			return
		}
		h.log.Error("failed to consume and verify email", "email", emailAddr, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
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

	_, err = h.sessions.Create(c.Request.Context(), user.ID, refreshToken, time.Now().Add(refreshTokenExpiry))
	if err != nil {
		h.log.Error("failed to create session", "user_id", userIDStr, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	if err := h.verifyLimiter.Reset(c.Request.Context(), emailAddr); err != nil {
		h.log.Warn("failed to reset verify limiter", "err", err)
	}
	if err := h.registerLimiter.Reset(c.Request.Context(), emailAddr); err != nil {
		h.log.Warn("failed to reset register limiter", "err", err)
	}
	h.log.Info("user verified and logged in", "user_id", userIDStr)

	data := api.LocalAuthData{
		User: api.AuthUser{
			Id:              user.ID,
			Email:           user.Email,
			Name:            user.Name,
			UserType:        api.AuthUserUserTypeClient,
			ProfileComplete: user.Name != "",
		},
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int(accessTokenTTL / time.Second),
	}
	c.JSON(http.StatusOK, api.NewSuccess("Email verified successfully", api.CodeOK, data))
}

// SignIn handles POST /auth/login.
func (h *LocalHandler) SignIn(c *gin.Context) {
	var req api.HandleLocalAuthJSONRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	emailAddr := strings.ToLower(strings.TrimSpace(string(req.Email)))

	if len(emailAddr) > 255 {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "email", Message: "email must not exceed 255 characters"},
		}))
		return
	}
	if !common.IsValidEmail(emailAddr) {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "email", Message: "invalid email format"},
		}))
		return
	}
	user, err := h.users.FindByEmail(c.Request.Context(), emailAddr)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusUnauthorized, api.NewError("Invalid email address", api.CodeUnauthorized))
			return
		}
		h.log.Error("sign-in: failed to find user", "email", emailAddr, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("Invalid email address", api.CodeServerError))
		return
	}

	if !user.IsActive {
		c.JSON(http.StatusUnauthorized, api.NewError("account not verified — please complete email verification first", api.CodeUnauthorized))
		return
	}

	userIDStr := user.ID.String()
	accessToken, err := GenerateJWTToken(userIDStr, AccessToken)
	if err != nil {
		h.log.Error("sign-in: failed to generate access token", "user_id", userIDStr, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	refreshToken, err := GenerateJWTToken(userIDStr, RefreshToken)
	if err != nil {
		h.log.Error("sign-in: failed to generate refresh token", "user_id", userIDStr, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	_, err = h.sessions.Create(c.Request.Context(), user.ID, refreshToken, time.Now().Add(refreshTokenExpiry))
	if err != nil {
		h.log.Error("sign-in: failed to create session", "user_id", userIDStr, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	h.log.Info("user signed in", "user_id", userIDStr)

	data := api.LocalAuthData{
		User: api.AuthUser{
			Id:              user.ID,
			Email:           user.Email,
			Name:            user.Name,
			UserType:        api.AuthUserUserTypeClient,
			ProfileComplete: user.Name != "",
		},
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int(accessTokenTTL / time.Second),
	}
	c.JSON(http.StatusOK, api.NewSuccess("Login successful", api.CodeOK, data))
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
