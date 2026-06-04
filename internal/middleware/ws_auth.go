package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	appredis "github.com/hngprojects/personal-trainer-be/pkg/redis"
)

func WebSocketAuthMiddleware(redis appredis.RedisClient, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr := c.GetHeader("Authorization")
		if tokenStr != "" {
			parts := strings.Split(tokenStr, " ")
			if len(parts) == 2 && parts[0] == "Bearer" {
				tokenStr = parts[1]
			}
		} else {
			tokenStr = c.Query("token")
		}

		if tokenStr == "" {
			log.Warn("WebSocketAuthMiddleware: missing token")
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("missing token", api.CodeUnauthorized))
			return
		}

		token, err := auth.ValidateToken(tokenStr)
		if err != nil || !token.Valid {
			log.Warn("WebSocketAuthMiddleware: invalid token", "err", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("invalid token", api.CodeUnauthorized))
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			log.Warn("WebSocketAuthMiddleware: token claims not MapClaims")
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("invalid token claims", api.CodeUnauthorized))
			return
		}

		tokenType, _ := claims["type"].(string)
		if tokenType != string(auth.AccessToken) {
			log.Warn("WebSocketAuthMiddleware: invalid token type", "expected", auth.AccessToken, "got", tokenType)
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("invalid token type", api.CodeUnauthorized))
			return
		}

		jti, _ := claims["jti"].(string)
		userID, _ := claims["sub"].(string)

		if redis != nil && jti != "" {
			blocked, err := redis.Exists(c.Request.Context(), common.RedisKeyBlocklist+jti)
			if err != nil {
				log.Warn("WebSocketAuthMiddleware: redis check failed", "err", err)
				c.AbortWithStatusJSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
				return
			}
			if blocked {
				log.Warn("WebSocketAuthMiddleware: revoked token used", "jti", jti)
				c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("token has been revoked", api.CodeUnauthorized))
				return
			}
		}

		parsedUserID, err := uuid.Parse(userID)
		if err != nil {
			log.Warn("WebSocketAuthMiddleware: invalid user id in token claims", "sub", userID, "err", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("invalid user id in token", api.CodeUnauthorized))
			return
		}

		c.Set(string(common.ContextKeyUserID), parsedUserID)
		c.Set(string(common.ContextKeyJTI), jti)
		c.Set(string(common.ContextKeyExpTime), int64(0))

		c.Next()
	}
}
