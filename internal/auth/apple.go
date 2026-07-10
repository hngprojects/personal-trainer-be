package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/apple"
	"github.com/hngprojects/personal-trainer-be/pkg/cryptoutil"
	pkgerrors "github.com/hngprojects/personal-trainer-be/pkg/errors"
)

// SIWARefreshTokenWriter persists an encrypted Apple refresh token on
// the user row. Provided as an interface so the handler doesn't have
// to know which sqlc query is doing the work (the wiring in routes.go
// supplies an adapter that calls SetUserAppleRefreshToken).
type SIWARefreshTokenWriter interface {
	SetUserAppleRefreshToken(ctx context.Context, userID uuid.UUID, encryptedToken string) error
}

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

	// oauth / cryptoBox / refreshStore are all optional. When all
	// three are non-nil AND the client supplied an authorization_code,
	// the handler exchanges it for a refresh token at sign-in and
	// persists the AES-GCM-encrypted result on the user row. Required
	// for the revoke-on-delete flow (App Store Review Guideline
	// 5.1.1 (v)). When any is nil, sign-in still works — the
	// exchange is silently skipped.
	oauth        *apple.SIWAOAuth
	cryptoBox    *cryptoutil.AESGCM
	refreshStore SIWARefreshTokenWriter
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

// WithOAuth wires the server-to-server pieces needed for
// revoke-on-delete. Optional — handler ignores it if any argument is
// nil, which means the legacy identity-token-only flow keeps working
// without these dependencies.
func (h *AppleHandler) WithOAuth(oauth *apple.SIWAOAuth, cryptoBox *cryptoutil.AESGCM, refreshStore SIWARefreshTokenWriter) *AppleHandler {
	h.oauth = oauth
	h.cryptoBox = cryptoBox
	h.refreshStore = refreshStore
	return h
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

	// If the mobile client included the authorization_code AND we have
	// the server-to-server OAuth pieces wired up, exchange the code for
	// a refresh token and persist it (encrypted). Apple issues the
	// authorization_code ONCE on the first authorization per device —
	// subsequent sign-ins won't carry it and we silently skip.
	//
	// Best-effort by design: any failure here (Apple unreachable,
	// already-consumed code, crypto error) logs a warning but doesn't
	// block sign-in. The user just won't have revoke-on-delete wired
	// up until they sign in again with a fresh authorization_code.
	if req.AuthorizationCode != nil {
		h.captureRefreshToken(c.Request.Context(), user.ID, strings.TrimSpace(*req.AuthorizationCode))
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

// captureRefreshToken exchanges Apple's authorization_code for a
// refresh token via /auth/token, encrypts the result with AES-GCM,
// and persists it on the user row. Any failure is logged and
// swallowed — refresh-token capture is purely additive (the absence
// just means revoke-on-delete won't fire for that user), so it must
// never break sign-in.
func (h *AppleHandler) captureRefreshToken(ctx context.Context, userID uuid.UUID, authCode string) {
	if authCode == "" {
		return
	}
	if h.oauth == nil || h.cryptoBox == nil || h.refreshStore == nil {
		// Config incomplete — the boot log already explained why; no
		// per-request noise is needed.
		return
	}
	tok, err := h.oauth.ExchangeAuthCode(ctx, authCode)
	if err != nil {
		h.log.Warn("apple sign-in: authorization_code exchange failed (revoke-on-delete will be unavailable for this user)",
			"user_id", userID.String(), "err", err)
		return
	}
	if tok == nil || tok.RefreshToken == "" {
		h.log.Warn("apple sign-in: token endpoint returned empty refresh_token", "user_id", userID.String())
		return
	}
	enc, err := h.cryptoBox.Encrypt([]byte(tok.RefreshToken))
	if err != nil {
		h.log.Warn("apple sign-in: refresh-token encryption failed", "user_id", userID.String(), "err", err)
		return
	}
	if err := h.refreshStore.SetUserAppleRefreshToken(ctx, userID, enc); err != nil {
		h.log.Warn("apple sign-in: failed to persist refresh token", "user_id", userID.String(), "err", err)
		return
	}
	h.log.Info("apple sign-in: refresh token captured", "user_id", userID.String())
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
