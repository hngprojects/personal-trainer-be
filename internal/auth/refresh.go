package auth

import (
	"net/http"
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

func (h *RefreshHandler) HandleRefresh(c *gin.Context) {
	var body struct {
		AccessToken string `json:"access_token"`
	}

	if err := c.ShouldBindJSON(&body); err != nil || body.AccessToken == "" {
		c.JSON(http.StatusBadRequest, api.NewError("access token is required", api.CodeBadRequest))
		return
	}

	if h.limiter != nil {
		refreshTokenString := c.GetString(string(api.BearerAuthScopes))
		if refreshTokenString != "" {
			if token, err := ValidateRefreshToken(refreshTokenString); err == nil && token.Valid {
				if claims, ok := token.Claims.(jwt.MapClaims); ok {
					if userID, ok := claims["sub"].(string); ok && userID != "" {
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
				}
			}
		}
	}

	refreshTokenString := c.GetString(string(api.BearerAuthScopes))
	if refreshTokenString == "" {
		c.JSON(http.StatusUnauthorized, api.NewError("unauthorized", api.CodeUnauthorized))
		return
	}

	refreshToken, err := ValidateRefreshToken(refreshTokenString)
	if err != nil || !refreshToken.Valid {
		c.JSON(http.StatusUnauthorized, api.NewError("unauthorized", api.CodeUnauthorized))
		return
	}

	refreshClaims, ok := refreshToken.Claims.(jwt.MapClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("unauthorized", api.CodeUnauthorized))
		return
	}

	userID, ok := refreshClaims["sub"].(string)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, api.NewError("unauthorized", api.CodeUnauthorized))
		return
	}

	accessToken, _ := ValidateAccessToken(body.AccessToken)

	if accessToken != nil && accessToken.Valid {
		accessClaims, ok := accessToken.Claims.(jwt.MapClaims)
		if ok {
			jti, _ := accessClaims["jti"].(string)
			if jti != "" {
				exp, _ := accessClaims["exp"].(float64)
				remainingTTL := time.Until(time.Unix(int64(exp), 0))
				if remainingTTL > 0 {
					if err := h.redis.Set(c.Request.Context(), common.RedisKeyBlocklist+jti, 1, remainingTTL); err != nil {
						h.log.Error("failed to blocklist access token", "err", err)
						c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
						return
					}
				}
			}
		}
	}

	// Generate new access token
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
