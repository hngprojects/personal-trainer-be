package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	auth "github.com/hngprojects/personal-trainer-be/internal/service"
)

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")

		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			}{
				Status:  "error",
				Message: "Unauthorized; Missing token",
			})
			return
		}

		parts := strings.Split(header, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			}{
				Status:  "error",
				Message: "Unauthorized; Invalid token",
			})
			return
		}

		token, err := auth.ValidateToken(parts[1], auth.AccessToken)
		if err != nil || !token.Valid {
			slog.Error("validateToken", "error", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			}{
				Status:  "error",
				Message: "Unauthorized; Invalid token",
			})
			return
		}

		c.Next()
	}
}
