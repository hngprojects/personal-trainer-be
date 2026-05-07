package middleware

import (
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var sensitiveParams = []string{
	"password",
	"token",
	"access_token",
	"refresh_token",
	"code",
	"state",
	"secret",
	"client_secret",
	"api_key",
	"apikey",
	"auth",
	"credential",
	"private_key",
	"jwt",
}

func Logger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		requestID := c.GetString("request_id")

		path := c.Request.URL.Path
		if query := c.Request.URL.RawQuery; query != "" {
			path = path + "?" + filterQueryParams(query)
		}

		if status >= 500 {
			log.Error("server error",
				slog.String("method", c.Request.Method),
				slog.String("uri", path),
				slog.Int("status", status),
				slog.Int64("latency", latency.Milliseconds()),
				slog.String("request_id", requestID),
			)
		} else if status >= 400 {
			log.Warn("client error",
				slog.String("method", c.Request.Method),
				slog.String("uri", path),
				slog.Int("status", status),
				slog.Int64("latency", latency.Milliseconds()),
				slog.String("request_id", requestID),
			)
		} else {
			log.Info("request completed",
				slog.String("method", c.Request.Method),
				slog.String("uri", path),
				slog.Int("status", status),
				slog.Int64("latency", latency.Milliseconds()),
				slog.String("request_id", requestID),
			)
		}
	}
}

func filterQueryParams(query string) string {
	values, err := url.ParseQuery(query)
	if err != nil {
		return query
	}

	for key := range values {
		if slices.Contains(sensitiveParams, strings.ToLower(key)) {
			values.Set(key, "REDACTED")
		}
	}

	return values.Encode()
}
