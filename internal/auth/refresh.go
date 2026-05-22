package auth

import (
	"log/slog"
	"net/http"
	"reflect"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
	if redis != nil && reflect.ValueOf(redis).IsNil() {
		redis = nil
	}
	return &RefreshHandler{redis: redis, log: log, limiter: limiter}
}

func (h *RefreshHandler) HandleRefresh(c *gin.Context) {
	userID := c.MustGet("user_id").(uuid.UUID)
	jti := c.MustGet("jti").(string)
	expRaw, exists := c.Get(string(common.ContextKeyExpTime))
	exp, _ := expRaw.(int64)
	if !exists || exp == 0 {
		h.log.Error("missing or invalid exp in token context")
		c.JSON(http.StatusUnauthorized, api.NewError("invalid token", api.CodeUnauthorized))
		return
	}
	remainingTTL := time.Until(time.Unix(exp, 0))

	if h.limiter != nil {
		allowed, err := h.limiter.Allow(c.Request.Context(), userID.String())
		if err != nil {
			h.log.Error("rate limit check failed", "err", err)
			c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
			return
		}
		if !allowed {
			h.log.Warn("HandleRefresh: rate limit hit", "user_id", userID)
			c.JSON(http.StatusTooManyRequests, api.NewError("too many requests", api.CodeTooManyRequests))
			return
		}
	}
	if h.redis == nil {
		h.log.Error("token rotation unavailable: no Redis client configured")
		c.JSON(http.StatusInternalServerError, api.NewError("token rotation unavailable", api.CodeServerError))
		return
	}

	if err := h.redis.Set(c.Request.Context(), common.RedisKeyBlocklist+jti, 1, remainingTTL); err != nil {
		h.log.Error("failed to blocklist token", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	newAccessToken, err := GenerateJWTToken(userID.String(), AccessToken)
	if err != nil {
		h.log.Error("failed to generate access token", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	newRefreshToken, err := GenerateJWTToken(userID.String(), RefreshToken)
	if err != nil {
		h.log.Error("failed to generate refresh token", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccessResponse("access token refreshed", api.CodeOK, gin.H{
		"access_token":  newAccessToken,
		"refresh_token": newRefreshToken,
	}, nil))
}
