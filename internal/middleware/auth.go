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

func AuthMiddleware(redis appredis.RedisClient, log *slog.Logger) gin.HandlerFunc {
	return authMiddleware(redis, auth.AccessToken, log)
}

func AuthMiddlewareWithType(redis appredis.RedisClient, tokenType auth.TokenType, log *slog.Logger) gin.HandlerFunc {
	return authMiddleware(redis, tokenType, log)
}

func authMiddleware(redis appredis.RedisClient, expectedType auth.TokenType, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			log.Warn("AuthMiddleware: missing Authorization header")
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("missing token", api.CodeUnauthorized))
			return
		}

		parts := strings.Split(header, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			log.Warn("AuthMiddleware: invalid token format")
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("invalid token format", api.CodeUnauthorized))
			return
		}

		token, err := auth.ValidateToken(parts[1])
		if err != nil || !token.Valid {
			log.Warn("AuthMiddleware: invalid token", "err", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("invalid token", api.CodeUnauthorized))
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			log.Warn("AuthMiddleware: token claims not MapClaims")
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("invalid token claims", api.CodeUnauthorized))
			return
		}

		tokenType, _ := claims["type"].(string)
		expFloat, _ := claims["exp"].(float64) // jwt.MapClaims always decodes numbers as float64
		exp := int64(expFloat)                 // convert once; stored as int64 for RequestExpFromContext
		if tokenType != string(expectedType) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("invalid token type", api.CodeUnauthorized))
			return
		}

		jti, _ := claims["jti"].(string)
		userID, _ := claims["sub"].(string)

		if redis != nil && jti != "" {
			blocked, err := redis.Exists(c.Request.Context(), common.RedisKeyBlocklist+jti)
			if err != nil {
				log.Warn("AuthMiddleware: redis check failed", "err", err)
				c.AbortWithStatusJSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
				return
			}
			if blocked {
				log.Warn("AuthMiddleware: revoked token used", "jti", jti)
				c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("token has been revoked", api.CodeUnauthorized))
				return
			}
		}

		parsedUserID, err := uuid.Parse(userID)
		if err != nil {
			log.Warn("AuthMiddleware: invalid user id in token claims", "sub", userID, "err", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("invalid user id in token", api.CodeUnauthorized))
			return
		}

		c.Set(string(common.ContextKeyUserID), parsedUserID)
		c.Set(string(common.ContextKeyJTI), jti)
		if expectedType == "refresh" {
			c.Set(string(common.ContextKeyExpTime), exp) // now int64, not string
		}
		c.Next()
	}
}
