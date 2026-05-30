package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
	"github.com/hngprojects/personal-trainer-be/pkg/ratelimit"
)

// accountSetupTokenBytes is the entropy (in bytes) of the random token we
// generate before base64-url encoding. 32 bytes = 256 bits — comfortably
// past any brute-force horizon, even with the per-IP rate limiter offline.
const accountSetupTokenBytes = 32

// AccountSetupRepository persists the token rows and atomically swaps a
// supplied token for the user's new password hash. Implementations must
// guarantee single-use semantics: ConsumeTokenAndSetPassword may only
// succeed once per token.
type AccountSetupRepository interface {
	UpsertToken(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error
	ConsumeTokenAndSetPassword(ctx context.Context, tokenHash, hashedPassword string) (*db.User, error)
	TokenStatus(ctx context.Context, userID uuid.UUID) (consumed bool, exists bool, err error)
	// PeekToken returns the row's consumed_at / expires_at without touching
	// it. Used by the FE pre-flight to render the right state before the
	// trainer types a password. Returns exists=false on no-such-token.
	PeekToken(ctx context.Context, tokenHash string) (consumed bool, expiresAt time.Time, exists bool, err error)
}

type postgresAccountSetupRepo struct {
	rawDB *sql.DB
}

func NewPostgresAccountSetupRepo(rawDB *sql.DB) AccountSetupRepository {
	return &postgresAccountSetupRepo{rawDB: rawDB}
}

func (r *postgresAccountSetupRepo) UpsertToken(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	return db.New(r.rawDB).UpsertAccountSetupToken(ctx, db.UpsertAccountSetupTokenParams{
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
	})
}

// ConsumeTokenAndSetPassword atomically:
//  1. marks the matching token row consumed (returns ErrNotFound if missing,
//     already consumed, or expired)
//  2. updates the user's password
//  3. revokes all refresh sessions so the freshly-set password takes effect
//
// Wrapped in a single transaction so a partial outcome — e.g. token marked
// consumed but password update failed — can never leave the user locked out
// with no working credential and no way to retry.
func (r *postgresAccountSetupRepo) ConsumeTokenAndSetPassword(ctx context.Context, tokenHash, hashedPassword string) (*db.User, error) {
	tx, err := r.rawDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	q := db.New(tx)

	userID, err := q.ConsumeAccountSetupToken(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	user, err := q.UpdateUserPasswordByID(ctx, db.UpdateUserPasswordByIDParams{
		ID:       userID,
		Password: sql.NullString{String: hashedPassword, Valid: true},
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// User got deactivated/deleted between token issue and consume.
			// Treat as ErrNotFound so the handler returns the same generic
			// "invalid or expired token" — never leak which branch missed.
			return nil, ErrNotFound
		}
		return nil, err
	}

	if err := q.DeleteSessionsByUserID(ctx, user.ID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *postgresAccountSetupRepo) TokenStatus(ctx context.Context, userID uuid.UUID) (consumed, exists bool, err error) {
	row, err := db.New(r.rawDB).GetAccountSetupTokenStatus(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, false, nil
		}
		return false, false, err
	}
	return row.ConsumedAt.Valid, true, nil
}

func (r *postgresAccountSetupRepo) PeekToken(ctx context.Context, tokenHash string) (consumed bool, expiresAt time.Time, exists bool, err error) {
	row, err := db.New(r.rawDB).PeekAccountSetupToken(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, time.Time{}, false, nil
		}
		return false, time.Time{}, false, err
	}
	return row.ConsumedAt.Valid, row.ExpiresAt, true, nil
}

// AccountSetupHandler owns POST /auth/set-password and exposes IssueToken
// to other packages (the trainer create flow) so they can mint a fresh
// token + email the link without re-implementing the HMAC / expiry math.
type AccountSetupHandler struct {
	repo         AccountSetupRepository
	mailer       email.Mailer
	log          *slog.Logger
	otpSecret    string
	frontendURL  string
	expiryHours  int
	ipLimiter    ratelimit.RateLimiter
}

func NewAccountSetupHandler(
	repo AccountSetupRepository,
	mailer email.Mailer,
	log *slog.Logger,
	otpSecret string,
	frontendURL string,
	expiryHours int,
	ipLimiter ratelimit.RateLimiter,
) *AccountSetupHandler {
	if expiryHours <= 0 {
		expiryHours = 168
	}
	if ipLimiter == nil {
		ipLimiter = ratelimit.AllowAllLimiter{}
	}
	return &AccountSetupHandler{
		repo:        repo,
		mailer:      mailer,
		log:         log,
		otpSecret:   otpSecret,
		frontendURL: frontendURL,
		expiryHours: expiryHours,
		ipLimiter:   ipLimiter,
	}
}

// IssueAndSend mints a fresh token for user_id, persists its HMAC, and
// emails the activation link. Returns an error if any of those steps
// fail — callers (CreateTrainer) decide whether to surface a 500 to the
// admin so they can retry.
//
// The recipient's display name is used only in the email greeting.
func (h *AccountSetupHandler) IssueAndSend(ctx context.Context, userID uuid.UUID, toEmail, name string) error {
	token, err := generateSetupToken()
	if err != nil {
		return fmt.Errorf("generate setup token: %w", err)
	}
	expiresAt := time.Now().Add(time.Duration(h.expiryHours) * time.Hour)
	if err := h.repo.UpsertToken(ctx, userID, h.hashToken(token), expiresAt); err != nil {
		return fmt.Errorf("persist setup token: %w", err)
	}
	link := h.buildSetupLink(token)
	if err := h.mailer.SendAccountSetupLink(toEmail, name, link, h.expiryHours); err != nil {
		return fmt.Errorf("send setup email: %w", err)
	}
	return nil
}

// IsActivated returns true iff the user already redeemed a previous setup
// token. CreateTrainer uses this to decide whether a re-invite is allowed
// (re-inviting an activated trainer is a 409 — admin should tell them to
// use the existing forgot-password flow instead).
func (h *AccountSetupHandler) IsActivated(ctx context.Context, userID uuid.UUID) (bool, error) {
	consumed, _, err := h.repo.TokenStatus(ctx, userID)
	return consumed, err
}

// HandleSetPassword handles POST /auth/set-password.
//
// Request: { "token": "...", "new_password": "..." }
//
// The handler returns the same generic 400 ("invalid or expired token")
// for every failure branch other than password validation so a probe
// can't distinguish "wrong token" from "expired" from "already consumed".
func (h *AccountSetupHandler) HandleSetPassword(c *gin.Context) {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Warn("HandleSetPassword: invalid request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}
	token := strings.TrimSpace(req.Token)
	newPassword := req.NewPassword

	var fieldErrors []api.FieldError
	if token == "" {
		fieldErrors = append(fieldErrors, api.FieldError{Field: "token", Message: "token is required"})
	}
	if msg, ok := validatePassword(newPassword); !ok {
		fieldErrors = append(fieldErrors, api.FieldError{Field: "new_password", Message: msg})
	}
	if len(fieldErrors) > 0 {
		h.log.Warn("HandleSetPassword: field validation failed", "field_errors", len(fieldErrors))
		c.JSON(http.StatusBadRequest, api.NewValidationError(fieldErrors))
		return
	}

	// IP-based rate limit caps brute-force token guessing. Per-token /
	// per-email rate limits aren't useful here because the handler never
	// receives the email (token is the only identifier) and we shouldn't
	// reveal which user a token maps to before we've validated it.
	if allowed, err := h.ipLimiter.Allow(c.Request.Context(), c.ClientIP()); err != nil {
		h.log.Warn("HandleSetPassword: IP rate limiter error — failing open", "err", err)
	} else if !allowed {
		h.log.Warn("HandleSetPassword: IP rate limit hit", "ip", c.ClientIP())
		c.JSON(http.StatusTooManyRequests, api.NewError("too many attempts, please try again later", api.CodeTooManyRequests))
		return
	}

	// Hash the new password BEFORE the DB lookup so wall-clock time stays
	// roughly constant whether the token is valid, expired, or garbage.
	// Mirrors the same constant-time strategy used by HandleResetPassword.
	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		h.log.Error("bcrypt hashing failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	user, err := h.repo.ConsumeTokenAndSetPassword(c.Request.Context(), h.hashToken(token), string(hashed))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			h.log.Warn("HandleSetPassword: invalid or expired setup token")
			c.JSON(http.StatusBadRequest, api.NewError("invalid or expired setup token", api.CodeBadRequest))
			return
		}
		h.log.Error("failed to consume account setup token", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	h.log.Info("account password set via setup token", "user_id", user.ID.String())
	c.JSON(http.StatusOK, api.NewSuccess("password set successfully", api.CodeOK, nil))
}

// HandleValidateSetupToken handles GET /trainers/set-password/validate?token=...
//
// Used by the FE set-password page as a pre-flight: it shows "your link
// expired", "this link was already used", or the password form depending
// on the response. Returns 200 with a small status payload in every
// success case (valid / consumed / expired / unknown) so the FE can
// branch on `data.status` without parsing error messages.
//
// IP rate-limited (same bucket as HandleSetPassword) so this endpoint
// can't be used to enumerate tokens any faster than the consume endpoint
// could be brute-forced.
func (h *AccountSetupHandler) HandleValidateSetupToken(c *gin.Context) {
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "token", Message: "token query parameter is required"},
		}))
		return
	}

	if allowed, err := h.ipLimiter.Allow(c.Request.Context(), c.ClientIP()); err != nil {
		h.log.Warn("HandleValidateSetupToken: IP rate limiter error — failing open", "err", err)
	} else if !allowed {
		h.log.Warn("HandleValidateSetupToken: IP rate limit hit", "ip", c.ClientIP())
		c.JSON(http.StatusTooManyRequests, api.NewError("too many attempts, please try again later", api.CodeTooManyRequests))
		return
	}

	consumed, expiresAt, exists, err := h.repo.PeekToken(c.Request.Context(), h.hashToken(token))
	if err != nil {
		h.log.Error("HandleValidateSetupToken: lookup failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	// status drives the FE's decision tree. Strings (not bools) so a new
	// state — e.g. "revoked" — could slot in later without a breaking
	// change to existing clients.
	//
	// Boundary: the consume query gates on `expires_at > NOW()`, so a token
	// at exactly its expiry instant is rejected there. Use `!Before` (i.e.
	// now >= expiresAt) here so this endpoint reports "expired" at the
	// same instant — otherwise the FE would render the password form,
	// the user would submit, and consume would 400 a moment later.
	var status string
	switch {
	case !exists:
		status = "invalid"
	case consumed:
		status = "consumed"
	case !time.Now().Before(expiresAt):
		status = "expired"
	default:
		status = "valid"
	}

	payload := map[string]any{
		"status": status,
		"valid":  status == "valid",
	}
	if exists {
		payload["expires_at"] = expiresAt
	}

	c.JSON(http.StatusOK, api.NewSuccess("setup token checked", api.CodeOK, payload))
}

func (h *AccountSetupHandler) hashToken(token string) string {
	mac := hmac.New(sha256.New, []byte(h.otpSecret))
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

// buildSetupLink renders the FE set-password URL. Honors a comma-separated
// FRONTEND_URL by using the first entry — same convention as the CORS
// middleware which treats FRONTEND_URL as an allowed-origin list.
//
// Path is /trainers/set-password to match the public API route
// (POST /trainers/set-password) so the FE page and the backend endpoint
// live at the same path — easier to reason about.
func (h *AccountSetupHandler) buildSetupLink(token string) string {
	base := strings.TrimRight(firstOrigin(h.frontendURL), "/")
	return base + "/trainers/set-password?token=" + token
}

func firstOrigin(s string) string {
	for _, part := range strings.Split(s, ",") {
		if v := strings.TrimSpace(part); v != "" {
			return v
		}
	}
	return ""
}

func generateSetupToken() (string, error) {
	buf := make([]byte, accountSetupTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
