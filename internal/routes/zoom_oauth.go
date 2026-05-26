// Hand-wired Zoom per-trainer OAuth + status endpoints.
//
// These routes are NOT in api.yaml + oapi-codegen because we want to
// be able to ship the feature independently of regenerating the
// generated handler interface; spec catch-up happens in a follow-up
// commit. Until then this file is the source of truth for the
// request/response shape — keep it small.
//
// Routes registered (all under /api/v1):
//   GET    /trainers/me/zoom/connect     → 302 to Zoom authorize URL
//   GET    /trainers/me/zoom/callback    → exchange code + persist
//   GET    /trainers/me/zoom/status      → connected? + email + expiry
//   DELETE /trainers/me/zoom             → drop credentials
package routes

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	appredis "github.com/hngprojects/personal-trainer-be/pkg/redis"
	"github.com/hngprojects/personal-trainer-be/pkg/zoom"
	"github.com/hngprojects/personal-trainer-be/internal/zoomflow"
)

// zoomOAuthRoutes is the seam between routes.go and the zoom OAuth
// handler. Implemented by either zoomOAuthHandler (real) or
// zoomDisabledHandler (env-not-configured no-op).
type zoomOAuthRoutes interface {
	register(group *gin.RouterGroup, authMw gin.HandlerFunc)
}

// zoomDisabledHandler is wired when the encryption key / OAuth creds
// are missing. Every endpoint 503s — boot stays up but the feature
// is unavailable. Cleaner than not registering the route (clients get
// 404 and can't tell missing-feature from typo).
type zoomDisabledHandler struct{}

func (zoomDisabledHandler) register(group *gin.RouterGroup, authMw gin.HandlerFunc) {
	disabled := func(c *gin.Context) {
		c.JSON(http.StatusServiceUnavailable, api.NewError("per-trainer Zoom integration is not configured on this server", api.CodeServerError))
	}
	group.GET("/trainers/me/zoom/connect", authMw, disabled)
	group.GET("/trainers/me/zoom/callback", disabled)
	group.GET("/trainers/me/zoom/status", authMw, disabled)
	group.DELETE("/trainers/me/zoom", authMw, disabled)
}

// zoomOAuthHandler is the real implementation; built only when the
// OAuth client + credential store are both ready.
type zoomOAuthHandler struct {
	store *zoomflow.CredentialStore
	oauth *zoom.OAuthClient
	redis *appredis.Client // for state CSRF tokens; may be nil → in-memory fallback
	log   *slog.Logger
	// inMem is the fallback state store when Redis isn't wired. NOT
	// safe across instances — production should always have Redis. Kept
	// for local dev where someone might run without it.
	inMem *stateMemory
}

func newZoomOAuthHandler(store *zoomflow.CredentialStore, oauth *zoom.OAuthClient, redis *appredis.Client, log *slog.Logger) zoomOAuthRoutes {
	return &zoomOAuthHandler{
		store: store,
		oauth: oauth,
		redis: redis,
		log:   log,
		inMem: newStateMemory(),
	}
}

const (
	stateTTL       = 10 * time.Minute
	stateRedisKey  = "zoom:oauth:state:"
	stateByteLen   = 32 // base64-encodes to ~43 chars; CSRF-resistant
)

func (h *zoomOAuthHandler) register(group *gin.RouterGroup, authMw gin.HandlerFunc) {
	group.GET("/trainers/me/zoom/connect", authMw, h.connect)
	// Callback intentionally has NO authMw — Zoom redirects the browser
	// here directly, no Authorization header is in play. We recover the
	// user identity from the signed/random state we stashed at connect
	// time.
	group.GET("/trainers/me/zoom/callback", h.callback)
	group.GET("/trainers/me/zoom/status", authMw, h.status)
	group.DELETE("/trainers/me/zoom", authMw, h.disconnect)
}

// connect mints a state nonce, stashes (state → userID), and returns
// the Zoom authorize URL. We return JSON (rather than 302) so the
// mobile app can open the URL in an in-app browser / system browser
// of its choice — same shape as how /auth/google/connect works
// elsewhere in the codebase.
func (h *zoomOAuthHandler) connect(c *gin.Context) {
	userID, ok := common.UserIDFromContext(c.Request.Context())
	if !ok || userID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}
	if !h.oauth.IsConfigured() {
		c.JSON(http.StatusServiceUnavailable, api.NewError("zoom oauth not configured", api.CodeServerError))
		return
	}
	state, err := newStateToken()
	if err != nil {
		h.log.Error("zoom connect: failed to mint state", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal error", api.CodeServerError))
		return
	}
	if err := h.saveState(c.Request.Context(), state, userID); err != nil {
		h.log.Error("zoom connect: failed to persist state", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal error", api.CodeServerError))
		return
	}
	c.JSON(http.StatusOK, api.NewSuccess("authorize the app on Zoom", api.CodeOK, gin.H{
		"authorize_url": h.oauth.AuthorizationURL(state),
	}))
}

// callback finishes the OAuth handshake. Lots of moving parts so the
// error paths are explicit — if anything fails we want operators to
// know which leg (state lookup, exchange, profile fetch, persist).
func (h *zoomOAuthHandler) callback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		c.JSON(http.StatusBadRequest, api.NewError("missing code or state", api.CodeBadRequest))
		return
	}
	userID, err := h.consumeState(c.Request.Context(), state)
	if err != nil {
		// Stale or unknown state — the user may have refreshed the
		// callback page or someone is hand-crafting URLs. Either way,
		// don't leak which it was.
		h.log.Warn("zoom callback: unknown or expired state")
		c.JSON(http.StatusBadRequest, api.NewError("state is unknown or expired — please retry the connect flow", api.CodeBadRequest))
		return
	}

	tokens, err := h.oauth.ExchangeCode(c.Request.Context(), code)
	if err != nil {
		h.log.Warn("zoom callback: token exchange failed", "err", err, "user_id", userID)
		c.JSON(http.StatusBadGateway, api.NewError("zoom rejected the authorization code", api.CodeServerError))
		return
	}

	profile, err := h.oauth.FetchUserProfile(c.Request.Context(), tokens.AccessToken)
	if err != nil {
		h.log.Warn("zoom callback: fetch profile failed", "err", err, "user_id", userID)
		// Don't bail — we can still persist tokens without the profile;
		// next refresh will work, and the user will see "connected"
		// without an email next to it. Better than forcing a reconnect
		// for a transient /users/me blip.
		profile = &zoom.UserProfile{}
	}

	if err := h.store.PersistFromExchange(c.Request.Context(), userID, tokens, profile); err != nil {
		h.log.Error("zoom callback: persist failed", "err", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to persist zoom credentials", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("zoom connected", api.CodeOK, gin.H{
		"zoom_email": profile.Email,
		"expires_at": tokens.ExpiresAt,
	}))
}

func (h *zoomOAuthHandler) status(c *gin.Context) {
	userID, ok := common.UserIDFromContext(c.Request.Context())
	if !ok || userID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}
	connected, email, expiresAt, err := h.store.Status(c.Request.Context(), userID)
	if err != nil {
		h.log.Error("zoom status: lookup failed", "err", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to read zoom status", api.CodeServerError))
		return
	}
	data := gin.H{"connected": connected}
	if connected {
		data["zoom_email"] = email
		data["access_token_expires_at"] = expiresAt
	}
	c.JSON(http.StatusOK, api.NewSuccess("ok", api.CodeOK, data))
}

func (h *zoomOAuthHandler) disconnect(c *gin.Context) {
	userID, ok := common.UserIDFromContext(c.Request.Context())
	if !ok || userID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}
	existed, err := h.store.Delete(c.Request.Context(), userID)
	if err != nil {
		h.log.Error("zoom disconnect: delete failed", "err", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to disconnect zoom", api.CodeServerError))
		return
	}
	if !existed {
		c.JSON(http.StatusNotFound, api.NewError("no zoom connection to disconnect", api.CodeNotFound))
		return
	}
	c.JSON(http.StatusOK, api.NewSuccess("zoom disconnected", api.CodeOK, nil))
}

func newStateToken() (string, error) {
	raw := make([]byte, stateByteLen)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// saveState writes state→userID to Redis (preferred) or in-memory.
func (h *zoomOAuthHandler) saveState(ctx context.Context, state string, userID uuid.UUID) error {
	if h.redis != nil {
		return h.redis.Set(ctx, stateRedisKey+state, userID.String(), stateTTL)
	}
	h.inMem.put(state, userID, stateTTL)
	return nil
}

// consumeState looks up and deletes state in one step (single-use).
// Without single-use, a leaked state could be replayed.
func (h *zoomOAuthHandler) consumeState(ctx context.Context, state string) (uuid.UUID, error) {
	if h.redis != nil {
		key := stateRedisKey + state
		raw, err := h.redis.Get(ctx, key).Result()
		if err != nil || raw == "" {
			return uuid.Nil, errors.New("state not found")
		}
		// Best-effort delete; even if it fails, the TTL will sweep it.
		_ = h.redis.Delete(ctx, key).Err()
		id, err := uuid.Parse(raw)
		if err != nil {
			return uuid.Nil, err
		}
		return id, nil
	}
	return h.inMem.consume(state)
}
