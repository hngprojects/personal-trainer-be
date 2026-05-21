package auth

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/api"
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
	userID := c.MustGet("user_id").(uuid.UUID)

	if h.limiter != nil {
		allowed, err := h.limiter.Allow(c.Request.Context(), userID.String())
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

	newAccessToken, err := GenerateJWTToken(userID.String(), AccessToken)
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
