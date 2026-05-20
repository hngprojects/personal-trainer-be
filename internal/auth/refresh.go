package auth

import (
	"net/http"
	"strings"
	"time"

	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/pkg/ratelimit"
	appredis "github.com/hngprojects/personal-trainer-be/pkg/redis"
)

type RefreshHandler struct {
	redis   appredis.RedisClient
	log     *slog.Logger
	limiter ratelimit.RateLimiter
}

func NewRefreshHandler(redis appredis.RedisClient, log *slog.Logger, limiter ratelimit.RateLimiter) *RefreshHandler {
	return &RefreshHandler{redis: redis, log: log, limiter: limiter}
}

// bearerTokenFromHeader extracts the token from "Authorization: Bearer <token>".
// Returns the empty string if the header is missing or malformed — the caller
// decides how to surface that.
//
// We read this directly here (rather than depend on the oapi-codegen-generated
// auth scope context value) because:
//   - api.BearerAuthScopes is a *marker key* set to []string{} that signals
//     "this route requires bearer auth"; it is NOT the bearer token value.
//     The previous code's c.GetString(string(api.BearerAuthScopes)) always
//     returned "" — making the refresh endpoint silently 401 for everyone.
//   - The standard auth middleware was also blocking us because it gates
//     tokenType == access. Refresh uses a *refresh* token, so /auth/refresh
//     is now declared security: [] in the OpenAPI spec; the handler is
//     responsible for its own token validation.
func bearerTokenFromHeader(c *gin.Context) string {
	header := c.GetHeader("Authorization")
	if header == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func (h *RefreshHandler) HandleRefresh(c *gin.Context) {
	var body struct {
		AccessToken string `json:"access_token"`
	}

	if err := c.ShouldBindJSON(&body); err != nil || body.AccessToken == "" {
		c.JSON(http.StatusBadRequest, api.NewError("access token is required", api.CodeBadRequest))
		return
	}

	refreshTokenString := bearerTokenFromHeader(c)
	if refreshTokenString == "" {
		c.JSON(http.StatusUnauthorized, api.NewError("refresh token missing from Authorization header", api.CodeUnauthorized))
		return
	}

	refreshToken, err := ValidateRefreshToken(refreshTokenString)
	if err != nil || !refreshToken.Valid {
		c.JSON(http.StatusUnauthorized, api.NewError("invalid or expired refresh token", api.CodeUnauthorized))
		return
	}

	refreshClaims, ok := refreshToken.Claims.(jwt.MapClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("invalid refresh token claims", api.CodeUnauthorized))
		return
	}

	userID, ok := refreshClaims["sub"].(string)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, api.NewError("refresh token missing subject claim", api.CodeUnauthorized))
		return
	}

	// Rate-limit by user ID once we know we have a valid refresh token.
	// Doing it after validation prevents an unauthenticated attacker from
	// burning another user's quota by spamming the endpoint with their JWT.
	if h.limiter != nil {
		allowed, err := h.limiter.Allow(c.Request.Context(), userID)
		if err != nil {
			h.log.Error("rate limit check failed", "err", err)
			c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
			return
		}
		if !allowed {
			c.JSON(http.StatusTooManyRequests, api.NewError("too many requests", api.CodeTooManyRequests))
			return
		}
	}

	// Blocklist the old access token so its remaining lifetime can't be
	// re-used after the rotation. The token may already be expired (common
	// case — that's WHY the client refreshed); ValidateAccessToken returns
	// an error but we still pull the jti+exp from claims to be defensive.
	accessToken, _ := ValidateAccessToken(body.AccessToken)
	if accessToken != nil && accessToken.Valid {
		if accessClaims, ok := accessToken.Claims.(jwt.MapClaims); ok {
			jti, _ := accessClaims["jti"].(string)
			if jti != "" {
				exp, _ := accessClaims["exp"].(float64)
				remainingTTL := time.Until(time.Unix(int64(exp), 0))
				if remainingTTL > 0 && h.redis != nil {
					if err := h.redis.Set(c.Request.Context(), common.RedisKeyBlocklist+jti, 1, remainingTTL); err != nil {
						h.log.Error("failed to blocklist access token", "err", err)
						c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
						return
					}
				}
			}
		}
	}

	newAccessToken, err := GenerateJWTToken(userID, AccessToken)
	if err != nil {
		h.log.Error("failed to generate access token", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccessResponse("access token refreshed", api.CodeOK, gin.H{
		"access_token": newAccessToken,
		"expires_in":   600, // 10 minutes
	}, nil))
}
