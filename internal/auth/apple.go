package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/apple"
	pkgerrors "github.com/hngprojects/personal-trainer-be/pkg/errors"
)

// AppleVerifier is the slice of pkg/apple.Verifier the handler depends
// on. Defined as an interface so tests can supply a stub without
// standing up a JWKS HTTP server or signing tokens.
type AppleVerifier interface {
	Verify(ctx context.Context, idToken string) (*apple.Claims, error)
}

// AppleHandler turns Sign-in-with-Apple identity tokens into FitCall
// JWT pairs. One endpoint serves both the mobile native flow
// (AuthenticationServices on iOS, REST POST on Android) and the web
// "Sign in with Apple JS" flow — both produce the same identity-token
// shape, so the server doesn't need separate routes the way Google
// does (the Google web flow is OAuth-code-exchange-based and needs a
// state cookie).
//
// Lookup strategy: Apple's `sub` claim is the only stable identifier —
// `email` is omitted on every sign-in after the first authorization,
// so a (email, auth_provider) lookup would fail every returning user.
// We therefore find/create by sub, and treat email + name as
// write-once bonuses populated on the first sign-in only.
type AppleHandler struct {
	users    UserRepository
	sessions SessionRepository
	verifier AppleVerifier
	log      *slog.Logger
}

func NewAppleHandler(cfg *config.Config, users UserRepository, sessions SessionRepository, verifier AppleVerifier, log *slog.Logger) *AppleHandler {
	if cfg != nil && len(cfg.AppleSignInBundleIDs) == 0 {
		log.Warn("apple sign-in: APPLE_SIGN_IN_BUNDLE_IDS (and APPLE_BUNDLE_ID fallback) are empty — endpoint will reject every token")
	}
	return &AppleHandler{
		users:    users,
		sessions: sessions,
		verifier: verifier,
		log:      log,
	}
}

// SignIn handles POST /auth/apple.
func (h *AppleHandler) SignIn(c *gin.Context) {
	var req api.HandleAppleSignInJSONRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Warn("apple sign-in: invalid request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	idToken := strings.TrimSpace(req.IdToken)
	if idToken == "" {
		h.log.Warn("apple sign-in: empty id_token")
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "id_token", Message: "id_token is required"},
		}))
		return
	}

	if h.verifier == nil {
		h.log.Warn("apple sign-in: verifier not configured")
		c.JSON(http.StatusServiceUnavailable, api.NewError("apple sign-in is not configured on this server", api.CodeServerError))
		return
	}

	claims, err := h.verifier.Verify(c.Request.Context(), idToken)
	if err != nil {
		h.log.Warn("apple sign-in: token verification failed", "err", err)
		c.JSON(http.StatusUnauthorized, api.NewError("invalid apple id token", api.CodeUnauthorized))
		return
	}

	// Apple only exposes the user's display name to the CLIENT on the
	// first authorization, never on the server-bound identity token.
	// The mobile app must therefore forward it in the request body on
	// that one sign-in. Empty thereafter is expected.
	name := ""
	if req.User != nil && req.User.Name != nil {
		name = strings.TrimSpace(*req.User.Name)
	}

	user, isNewUser, err := h.findOrCreate(c.Request.Context(), claims, name)
	if err != nil {
		h.log.Error("apple sign-in: user lookup/create failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	userIDStr := user.ID.String()
	accessToken, err := GenerateJWTToken(userIDStr, AccessToken)
	if err != nil {
		h.log.Error("apple sign-in: access token generation failed", "user_id", userIDStr, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	refreshToken, err := GenerateJWTToken(userIDStr, RefreshToken)
	if err != nil {
		h.log.Error("apple sign-in: refresh token generation failed", "user_id", userIDStr, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	if _, err := h.sessions.Create(c.Request.Context(), user.ID, refreshToken, time.Now().Add(refreshTokenExpiry)); err != nil {
		h.log.Error("apple sign-in: session create failed", "user_id", userIDStr, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	h.log.Info("apple sign-in successful",
		"user_id", userIDStr,
		"is_new_user", isNewUser,
		"is_private_email", claims.IsPrivateEmail,
	)

	authUser, _ := buildAuthUser(c.Request.Context(), h.users, user, h.log)
	// Reuse GoogleAuthData — same shape, no point duplicating it. The
	// FE branches on the route, not the payload type.
	data := api.GoogleAuthData{
		User:         authUser,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		IsNewUser:    isNewUser,
		ExpiresIn:    int(accessTokenTTL / time.Second),
	}
	c.JSON(http.StatusOK, api.NewSuccess("Apple authentication successful", api.CodeOK, data))
}

// findOrCreate resolves the Apple sub to a user row, creating one on
// first sign-in. We never link an Apple sub to a pre-existing
// local/Google account silently — that would let an attacker who
// controls an Apple ID claim someone else's account just because the
// email happens to match.
func (h *AppleHandler) findOrCreate(ctx context.Context, claims *apple.Claims, name string) (*db.User, bool, error) {
	u, err := h.users.FindByAppleSub(ctx, claims.Sub)
	if err == nil {
		return u, false, nil
	}
	if !errors.Is(err, ErrNotFound) && !errors.Is(err, pkgerrors.ErrNotFound) {
		return nil, false, err
	}

	// First sign-in for this Apple ID — create a row. Email may be
	// empty (subsequent sign-ins omit it) or a private-relay address
	// (@privaterelay.appleid.com); both are valid and stored verbatim.
	created, err := h.users.CreateAppleUser(ctx, claims.Email, name, claims.Sub)
	if err != nil {
		return nil, false, err
	}
	return created, true, nil
}
