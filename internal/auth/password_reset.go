package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
	"github.com/hngprojects/personal-trainer-be/pkg/ratelimit"
)

const (
	passwordResetCodeExpiry = 10 * time.Minute
	bcryptCost              = 12
	minPasswordLen          = 8
	// bcrypt.GenerateFromPassword silently truncates inputs above 72 bytes (and
	// in newer versions returns ErrPasswordTooLong). validatePassword measures
	// byte length (Go's len() on a string returns bytes), so capping here at 72
	// guarantees a clean 400 validation error instead of letting a 60-character
	// multi-byte UTF-8 password (>72 bytes) sneak through and crash bcrypt with
	// a 500 at hash time.
	maxPasswordLen     = 72
	adminRoleName      = "admin"
	superAdminRoleName = "super_admin"
	trainerRoleName    = "trainer"
	forgotAsyncTimeout = 30 * time.Second
)

// PasswordResetRepository encapsulates the persistence side of the password
// reset flow. UpsertCode is exposed (rather than separate Delete/Create methods)
// so that clearing the previous code and inserting the new one happen inside a
// single transaction — preventing concurrent forgot-password requests from
// leaving multiple valid codes in the table.
type PasswordResetRepository interface {
	UpsertCode(ctx context.Context, email, hashedCode string, expiresAt time.Time) error
	VerifyCode(ctx context.Context, email, hashedCode string) error
	ConsumeCodeAndUpdatePassword(ctx context.Context, email, hashedCode, hashedPassword string) (*db.User, error)
}

type postgresPasswordResetRepo struct {
	rawDB *sql.DB
}

func NewPostgresPasswordResetRepo(rawDB *sql.DB) PasswordResetRepository {
	return &postgresPasswordResetRepo{rawDB: rawDB}
}

// UpsertCode delegates to the single-statement INSERT … ON CONFLICT (email)
// in the SQL layer. There is no read-modify-write window and no possibility
// of two valid codes coexisting for the same email — the UNIQUE(email)
// constraint on password_reset_codes guarantees it.
func (r *postgresPasswordResetRepo) UpsertCode(ctx context.Context, email, hashedCode string, expiresAt time.Time) error {
	return db.New(r.rawDB).UpsertPasswordResetCode(ctx, db.UpsertPasswordResetCodeParams{
		Email:     email,
		Code:      hashedCode,
		ExpiresAt: expiresAt,
	})
}

func (r *postgresPasswordResetRepo) VerifyCode(ctx context.Context, email, hashedCode string) error {
	_, err := db.New(r.rawDB).VerifyPasswordResetCode(ctx, db.VerifyPasswordResetCodeParams{Email: email, Code: hashedCode})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (r *postgresPasswordResetRepo) ConsumeCodeAndUpdatePassword(ctx context.Context, email, hashedCode, hashedPassword string) (*db.User, error) {
	tx, err := r.rawDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	q := db.New(tx)

	if _, err := q.ConsumePasswordResetCode(ctx, db.ConsumePasswordResetCodeParams{Email: email, Code: hashedCode}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	user, err := q.UpdateUserPassword(ctx, db.UpdateUserPasswordParams{Email: email, Password: sql.NullString{Valid: true, String: hashedPassword}})
	if err != nil {
		// UpdateUserPassword's WHERE clause requires is_active = true. If the
		// account was deactivated between role check and update, this returns
		// no rows — surface as ErrNotFound so the handler returns the same
		// generic message it returns for an invalid/expired code.
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// Revoke any existing refresh sessions so the changed password actually takes effect.
	if err := q.DeleteSessionsByUserID(ctx, user.ID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &user, nil
}

type PasswordResetHandler struct {
	users           UserRepository
	roles           RoleRepository
	resetRepo       PasswordResetRepository
	mailer          email.Mailer
	log             *slog.Logger
	forgotLimiter   ratelimit.RateLimiter // per-email: prevents one address being spammed (incl. via botnet)
	forgotIPLimiter ratelimit.RateLimiter // per-IP: prevents a single source enumerating many addresses
	resetLimiter    ratelimit.RateLimiter // per-email: caps brute-force attempts on a code
	resetIPLimiter  ratelimit.RateLimiter // per-IP: caps cross-account guessing from one source
	otpSecret       string
}

func NewPasswordResetHandler(
	users UserRepository,
	roles RoleRepository,
	resetRepo PasswordResetRepository,
	mailer email.Mailer,
	log *slog.Logger,
	otpSecret string,
	forgotLimiter ratelimit.RateLimiter,
	forgotIPLimiter ratelimit.RateLimiter,
	resetLimiter ratelimit.RateLimiter,
	resetIPLimiter ratelimit.RateLimiter,
) *PasswordResetHandler {
	return &PasswordResetHandler{
		users:           users,
		roles:           roles,
		resetRepo:       resetRepo,
		mailer:          mailer,
		log:             log,
		otpSecret:       otpSecret,
		forgotLimiter:   forgotLimiter,
		forgotIPLimiter: forgotIPLimiter,
		resetLimiter:    resetLimiter,
		resetIPLimiter:  resetIPLimiter,
	}
}

// HandleForgotPassword always returns the same generic success response,
// regardless of whether the email is registered or whether the user is an
// admin. To keep the *response time* constant too, the user lookup, role
// check, and email send all happen asynchronously in a detached goroutine —
// otherwise an attacker could distinguish "admin email" from "non-admin or
// unknown email" by the latency of the SMTP call.
func (h *PasswordResetHandler) HandleForgotPassword(c *gin.Context) {
	var req api.HandleForgotPasswordJSONRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Warn("forgot password: invalid request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	emailAddr := strings.ToLower(strings.TrimSpace(string(req.Email)))
	if len(emailAddr) > 255 || !common.IsValidEmail(emailAddr) {
		h.log.Warn("forgot password: invalid email", "email_domain", emailDomain(emailAddr))
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "email", Message: "invalid email format"},
		}))
		return
	}

	// IP-based rate limit: synchronous and visible (429). The key is the
	// caller's IP, never the input email — so triggering this 429 cannot leak
	// anything about the email being probed.
	if allowed, err := h.forgotIPLimiter.Allow(c.Request.Context(), c.ClientIP()); err != nil {
		h.log.Warn("forgot-password IP rate limiter error — failing open", "err", err)
	} else if !allowed {
		h.log.Warn("forgot password: IP rate limit hit", "clientIP", c.ClientIP())
		c.JSON(http.StatusTooManyRequests, api.NewError("too many requests, please try again later", api.CodeTooManyRequests))
		return
	}

	// Email-based rate limit is enforced inside the async pipeline below so
	// that a hit on it is silently dropped (still a 200) — that prevents an
	// attacker from learning anything about an email by spamming requests
	// against it and watching for a 429.
	go h.processForgotPassword(emailAddr)

	c.JSON(http.StatusOK, api.NewSuccess("if the email is registered, a reset code has been sent", api.CodeOK, nil))
}

// processForgotPassword runs the lookup → role check → email send pipeline
// detached from the request context (which would otherwise be canceled the
// moment we send the response). All early exits are silent — the success
// response has already been sent — and errors are logged but never surface
// to the client.
//
// Runs in a goroutine and is therefore outside the gin recover middleware:
// the explicit recover here keeps a panic from taking down the server.
func (h *PasswordResetHandler) processForgotPassword(emailAddr string) {
	defer func() {
		if rec := recover(); rec != nil {
			h.log.Error("panic in async forgot-password pipeline", "panic", rec)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), forgotAsyncTimeout)
	defer cancel()

	// Per-email rate limit. A hit silently no-ops — the synchronous side has
	// already returned 200 to the client, so the limit cannot be observed.
	if allowed, err := h.forgotLimiter.Allow(ctx, emailAddr); err != nil {
		h.log.Warn("forgot-password email rate limiter error — failing open", "err", err)
	} else if !allowed {
		return
	}

	user, err := h.users.FindByEmailAndProvider(ctx, emailAddr, providerLocal)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			h.log.Error("failed to look up user for password reset", "err", err)
		}
		return
	}
	if user == nil || !user.IsActive {
		return
	}

	// Check role directly from the users row — UserHasRole queries user_roles
	// which is not populated by the admin-provisioned trainer/admin creation flows.
	isAllowed := user.Role == adminRoleName || user.Role == superAdminRoleName || user.Role == trainerRoleName
	if !isAllowed {
		return
	}

	if err := h.issueResetCode(ctx, emailAddr); err != nil {
		h.log.Error("failed to issue password reset code", "err", err)
	}
}

func (h *PasswordResetHandler) issueResetCode(ctx context.Context, emailAddr string) error {
	code, err := generateVerificationCode()
	if err != nil {
		return fmt.Errorf("generate code: %w", err)
	}

	if err := h.resetRepo.UpsertCode(ctx, emailAddr, h.hashCode(code), time.Now().Add(passwordResetCodeExpiry)); err != nil {
		return fmt.Errorf("upsert code: %w", err)
	}

	if err := h.mailer.SendPasswordResetCode(emailAddr, code, int(passwordResetCodeExpiry.Minutes())); err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	return nil
}

func (h *PasswordResetHandler) HandleResetPassword(c *gin.Context) {
	var req api.HandleResetPasswordJSONRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Warn("reset password: invalid request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	emailAddr := strings.ToLower(strings.TrimSpace(string(req.Email)))
	code := strings.TrimSpace(req.Code)
	newPassword := req.NewPassword

	var fieldErrors []api.FieldError
	if len(emailAddr) > 255 || !common.IsValidEmail(emailAddr) {
		fieldErrors = append(fieldErrors, api.FieldError{Field: "email", Message: "invalid email format"})
	}
	if len(code) != 6 || !isDigits(code) {
		fieldErrors = append(fieldErrors, api.FieldError{Field: "code", Message: "code must be a 6-digit number"})
	}
	if msg, ok := validatePassword(newPassword); !ok {
		fieldErrors = append(fieldErrors, api.FieldError{Field: "new_password", Message: msg})
	}
	if len(fieldErrors) > 0 {
		h.log.Warn("reset password: field validation failed", "email_domain", emailDomain(emailAddr), "fieldErrors", fieldErrors)
		c.JSON(http.StatusBadRequest, api.NewValidationError(fieldErrors))
		return
	}

	// IP-based rate limit caps cross-account guessing from one source. Keyed
	// on the caller's IP (not the email), so a 429 here doesn't depend on
	// whether the email is registered.
	if allowed, err := h.resetIPLimiter.Allow(c.Request.Context(), c.ClientIP()); err != nil {
		h.log.Warn("reset-password IP rate limiter error — failing open", "err", err)
	} else if !allowed {
		h.log.Warn("reset password: IP rate limit hit", "clientIP", c.ClientIP())
		c.JSON(http.StatusTooManyRequests, api.NewError("too many attempts, please try again later", api.CodeTooManyRequests))
		return
	}

	if allowed, err := h.resetLimiter.Allow(c.Request.Context(), emailAddr); err != nil {
		h.log.Warn("reset-password email rate limiter error — failing open", "err", err)
	} else if !allowed {
		h.log.Warn("reset password: email rate limit hit", "email_domain", emailDomain(emailAddr))
		c.JSON(http.StatusTooManyRequests, api.NewError("too many attempts, please request a new code", api.CodeTooManyRequests))
		return
	}

	// Hash the new password BEFORE any DB lookup so that bcrypt's ~150ms cost
	// is paid by every well-formed request — keeping wall-clock time roughly
	// constant whether or not the email exists, the user is admin, etc.
	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		h.log.Error("bcrypt hashing failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	// Role gate. Every non-success branch returns the same generic error so a
	// probe cannot tell admin from non-admin from non-existent.
	user, err := h.users.FindByEmailAndProvider(c.Request.Context(), emailAddr, providerLocal)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			h.log.Warn("reset password: user not found", "email_domain", emailDomain(emailAddr))
			c.JSON(http.StatusBadRequest, api.NewError("invalid or expired reset code", api.CodeBadRequest))
			return
		}
		h.log.Error("failed to look up user for password reset", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	if !user.IsActive {
		h.log.Warn("reset password: inactive user", "email_domain", emailDomain(emailAddr), "user_id", user.ID.String())
		c.JSON(http.StatusBadRequest, api.NewError("invalid or expired reset code", api.CodeBadRequest))
		return
	}
	// Check role directly from the users row — UserHasRole queries user_roles
	// which is not populated by admin-provisioned trainer/admin creation flows.
	if user.Role != adminRoleName && user.Role != superAdminRoleName && user.Role != trainerRoleName {
		h.log.Warn("reset password: user is not admin or trainer", "email_domain", emailDomain(emailAddr), "user_id", user.ID.String())
		c.JSON(http.StatusBadRequest, api.NewError("invalid or expired reset code", api.CodeBadRequest))
		return
	}

	updated, err := h.resetRepo.ConsumeCodeAndUpdatePassword(c.Request.Context(), emailAddr, h.hashCode(code), string(hashed))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			h.log.Warn("reset password: code consume failed", "email_domain", emailDomain(emailAddr))
			c.JSON(http.StatusBadRequest, api.NewError("invalid or expired reset code", api.CodeBadRequest))
			return
		}
		h.log.Error("failed to reset password", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	// Reset only per-email buckets on success. The per-IP bucket is left to age
	// out — clearing it would let an attacker with one valid admin reset code
	// refresh the shared IP budget and keep guessing other accounts' codes from
	// the same source.
	if err := h.resetLimiter.Reset(c.Request.Context(), emailAddr); err != nil {
		h.log.Warn("failed to reset reset-password limiter", "err", err)
	}
	if err := h.forgotLimiter.Reset(c.Request.Context(), emailAddr); err != nil {
		h.log.Warn("failed to reset forgot-password limiter", "err", err)
	}

	h.log.Info("password reset successful", "user_id", updated.ID.String())
	c.JSON(http.StatusOK, api.NewSuccess("password reset successful", api.CodeOK, nil))
}

func (h *PasswordResetHandler) hashCode(code string) string {
	mac := hmac.New(sha256.New, []byte(h.otpSecret))
	mac.Write([]byte(code))
	return hex.EncodeToString(mac.Sum(nil))
}

func validatePassword(p string) (string, bool) {
	// Minimum is in characters (runes) so the error message matches user
	// intuition — len() would let a 5-char multi-byte UTF-8 password (15+ bytes)
	// satisfy a "must be at least 8 characters" check.
	if utf8.RuneCountInString(p) < minPasswordLen {
		return fmt.Sprintf("password must be at least %d characters", minPasswordLen), false
	}
	// Maximum is in bytes because bcrypt's 72-byte limit is a byte limit.
	if len(p) > maxPasswordLen {
		return fmt.Sprintf("password must not exceed %d bytes", maxPasswordLen), false
	}
	var hasUpper, hasLower, hasDigit bool
	for _, r := range p {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit {
		return "password must contain upper case, lower case, and a digit", false
	}
	return "", true
}

// HandleVerifyResetCode checks that an OTP code is valid and not expired
// without consuming it. Intended as a mid-flow step so mobile clients can
// confirm the code before navigating to the new-password screen.
func (h *PasswordResetHandler) HandleVerifyResetCode(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required"`
		Code  string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("email and code are required", api.CodeBadRequest))
		return
	}

	emailAddr := strings.ToLower(strings.TrimSpace(req.Email))
	code := strings.TrimSpace(req.Code)

	var fieldErrors []api.FieldError
	if len(emailAddr) > 255 || !common.IsValidEmail(emailAddr) {
		fieldErrors = append(fieldErrors, api.FieldError{Field: "email", Message: "invalid email format"})
	}
	if len(code) != 6 || !isDigits(code) {
		fieldErrors = append(fieldErrors, api.FieldError{Field: "code", Message: "code must be a 6-digit number"})
	}
	if len(fieldErrors) > 0 {
		c.JSON(http.StatusBadRequest, api.NewValidationError(fieldErrors))
		return
	}

	if allowed, err := h.resetIPLimiter.Allow(c.Request.Context(), c.ClientIP()); err != nil {
		h.log.Warn("verify-reset-code IP rate limiter error — failing open", "err", err)
	} else if !allowed {
		h.log.Warn("verify reset code: IP rate limit hit", "clientIP", c.ClientIP())
		c.JSON(http.StatusTooManyRequests, api.NewError("too many attempts, please try again later", api.CodeTooManyRequests))
		return
	}

	if allowed, err := h.resetLimiter.Allow(c.Request.Context(), emailAddr); err != nil {
		h.log.Warn("verify-reset-code email rate limiter error — failing open", "err", err)
	} else if !allowed {
		h.log.Warn("verify reset code: email rate limit hit", "email_domain", emailDomain(emailAddr))
		c.JSON(http.StatusTooManyRequests, api.NewError("too many attempts, please request a new code", api.CodeTooManyRequests))
		return
	}

	if err := h.resetRepo.VerifyCode(c.Request.Context(), emailAddr, h.hashCode(code)); err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusBadRequest, api.NewError("invalid or expired reset code", api.CodeBadRequest))
			return
		}
		h.log.Error("verify reset code: db error", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	// Mirror the eligibility gate from HandleResetPassword so this endpoint
	// never returns 200 for an account that the actual reset would reject.
	user, err := h.users.FindByEmailAndProvider(c.Request.Context(), emailAddr, providerLocal)
	if err != nil || !user.IsActive {
		c.JSON(http.StatusBadRequest, api.NewError("invalid or expired reset code", api.CodeBadRequest))
		return
	}
	// Check role directly from the users row — UserHasRole queries user_roles
	// which is not populated by admin-provisioned trainer/admin creation flows.
	if user.Role != adminRoleName && user.Role != superAdminRoleName && user.Role != trainerRoleName {
		c.JSON(http.StatusBadRequest, api.NewError("invalid or expired reset code", api.CodeBadRequest))
		return
	}

	// Reset the per-email bucket so a successful preflight doesn't consume
	// attempts that the caller needs for the actual reset step.
	if err := h.resetLimiter.Reset(c.Request.Context(), emailAddr); err != nil {
		h.log.Warn("failed to reset verify-reset-code limiter", "err", err)
	}

	c.JSON(http.StatusOK, api.NewSuccess("code is valid", api.CodeOK, nil))
}
