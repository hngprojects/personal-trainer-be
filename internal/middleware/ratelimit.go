package middleware

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/pkg/ratelimit"
)

// RateLimit returns a Gin middleware that limits requests by client IP.
// On Redis failure it fails open and logs a warning.
func RateLimit(limiter ratelimit.RateLimiter, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()

		allowed, err := limiter.Allow(c.Request.Context(), ip)
		if err != nil {
			log.Warn("rate limiter error — failing open", "err", err, "ip", ip)
			c.Next()
			return
		}

		if !allowed {
			log.Warn("rate limit exceeded", "ip", ip)
			c.AbortWithStatusJSON(http.StatusTooManyRequests, api.NewError(
				"too many requests, please slow down",
				api.CodeTooManyRequests,
			))
			return
		}

		c.Next()
	}
}
