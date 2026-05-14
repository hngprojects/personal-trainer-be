package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/api/idtoken"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	pkgerrors "github.com/hngprojects/personal-trainer-be/pkg/errors"
)

// MobileGoogleHandler verifies Google ID tokens issued to native mobile clients
// (Android / iOS) and exchanges them for our own JWTs. The mobile app obtains
// the ID token via the platform Sign-In SDK; the server only sees the result.
//
// This is intentionally a separate handler from the web OAuth flow in
// google.go: the web flow does the full code-exchange dance and relies on a
// state cookie, neither of which works for mobile.
type MobileGoogleHandler struct {
	users        UserRepository
	sessions     SessionRepository
	log          *slog.Logger
	allowedAuds  []string                                                                      // accepted `aud` claim values
	validateFunc func(ctx context.Context, idToken, audience string) (*idtoken.Payload, error) // swappable for tests
}

func NewMobileGoogleHandler(cfg *config.Config, users UserRepository, sessions SessionRepository, log *slog.Logger) *MobileGoogleHandler {
	// Accept any of: web, android, ios. Empty entries are filtered so missing
	// platforms don't accidentally allow tokens with empty audience claims.
	auds := make([]string, 0, 3)
	for _, id := range []string{cfg.GoogleAndroidClientID, cfg.GoogleIOSClientID, cfg.GoogleClientID} {
		if id = strings.TrimSpace(id); id != "" {
			auds = append(auds, id)
		}
	}
	if len(auds) == 0 {
		log.Warn("mobile google sign-in: no client IDs configured (GOOGLE_ANDROID_CLIENT_ID / GOOGLE_IOS_CLIENT_ID / GOOGLE_CLIENT_ID); endpoint will reject all tokens")
	}
	return &MobileGoogleHandler{
		users:        users,
		sessions:     sessions,
		log:          log,
		allowedAuds:  auds,
		validateFunc: idtoken.Validate,
	}
}

// SignIn handles POST /auth/google/mobile.
func (h *MobileGoogleHandler) SignIn(c *gin.Context) {
	var req api.HandleGoogleMobileSignInJSONRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	idToken := strings.TrimSpace(req.IdToken)
	if idToken == "" {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "id_token", Message: "id_token is required"},
		}))
		return
	}

	if len(h.allowedAuds) == 0 {
		// No client IDs configured — refuse rather than silently accept.
		c.JSON(http.StatusServiceUnavailable, api.NewError("google sign-in is not configured on this server", api.CodeServerError))
		return
	}

	payload, err := h.verifyAgainstAnyAudience(c.Request.Context(), idToken)
	if err != nil {
		// Don't leak whether the token was malformed, expired, or wrong audience.
		h.log.Warn("mobile google sign-in: token verification failed", "err", err)
		c.JSON(http.StatusUnauthorized, api.NewError("invalid google id token", api.CodeUnauthorized))
		return
	}

	emailRaw, _ := payload.Claims["email"].(string)
	emailVerified, _ := payload.Claims["email_verified"].(bool)
	name, _ := payload.Claims["name"].(string)

	emailAddr := strings.ToLower(strings.TrimSpace(emailRaw))
	if emailAddr == "" || !emailVerified {
		c.JSON(http.StatusUnauthorized, api.NewError("google account email is missing or unverified", api.CodeUnauthorized))
		return
	}

	isNewUser := false
	user, err := h.users.FindByEmailAndProvider(c.Request.Context(), emailAddr, "google")
	if err != nil {
		if !errors.Is(err, ErrNotFound) && !errors.Is(err, pkgerrors.ErrNotFound) {
			h.log.Error("mobile google sign-in: user lookup failed", "err", err)
			c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
			return
		}
		user, err = h.users.Create(c.Request.Context(), emailAddr, name, "google")
		if err != nil {
			h.log.Error("mobile google sign-in: user create failed", "err", err)
			c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
			return
		}
		isNewUser = true
	}

	userIDStr := user.ID.String()
	accessToken, err := GenerateJWTToken(userIDStr, AccessToken)
	if err != nil {
		h.log.Error("mobile google sign-in: access token generation failed", "user_id", userIDStr, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	refreshToken, err := GenerateJWTToken(userIDStr, RefreshToken)
	if err != nil {
		h.log.Error("mobile google sign-in: refresh token generation failed", "user_id", userIDStr, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	// Persist the refresh session so it can be revoked on logout, matching the
	// behaviour of the web Google flow's caller (and the local sign-in flow).
	if _, err := h.sessions.Create(c.Request.Context(), user.ID, refreshToken, time.Now().Add(refreshTokenExpiry)); err != nil {
		h.log.Error("mobile google sign-in: session create failed", "user_id", userIDStr, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	h.log.Info("mobile google sign-in successful", "user_id", userIDStr, "is_new_user", isNewUser)

	data := api.GoogleAuthData{
		User: api.AuthUser{
			Id:              user.ID,
			Email:           user.Email,
			Name:            user.Name,
			UserType:        api.Client,
			ProfileComplete: user.Name != "",
		},
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		IsNewUser:    isNewUser,
		ExpiresIn:    int(accessTokenTTL / time.Second),
	}
	c.JSON(http.StatusOK, api.NewSuccess("Google authentication successful", api.CodeOK, data))
}

// verifyAgainstAnyAudience runs idtoken.Validate against each configured
// audience and returns the payload from whichever one accepts. We can't pass
// a list to idtoken.Validate directly — it only takes a single audience.
func (h *MobileGoogleHandler) verifyAgainstAnyAudience(ctx context.Context, idToken string) (*idtoken.Payload, error) {
	var lastErr error
	for _, aud := range h.allowedAuds {
		payload, err := h.validateFunc(ctx, idToken, aud)
		if err == nil {
			return payload, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no configured audience accepted the token")
	}
	return nil, lastErr
}
