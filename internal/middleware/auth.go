package middleware

import (
	"log"
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

func AuthMiddleware(redis appredis.RedisClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("missing token", api.CodeUnauthorized))
			return
		}

		parts := strings.Split(header, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("invalid token format", api.CodeUnauthorized))
			return
		}

		token, err := auth.ValidateToken(parts[1])
		if err != nil || !token.Valid {
			log.Println("validate token err: ", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("invalid token", api.CodeUnauthorized))
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("invalid token claims", api.CodeUnauthorized))
			return
		}

		tokenType, _ := claims["type"].(string)
		if tokenType != string(auth.AccessToken) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("invalid token type", api.CodeUnauthorized))
			return
		}

		jti, _ := claims["jti"].(string)
		userID, _ := claims["sub"].(string)

		if redis != nil && jti != "" {
			blocked, err := redis.Exists(c.Request.Context(), common.RedisKeyBlocklist+jti)
			if err != nil {
				log.Println("redis check err: ", err)
				c.AbortWithStatusJSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
				return
			}
			if blocked {
				c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("token has been revoked", api.CodeUnauthorized))
				return
			}
		}

		parsedUserID, err := uuid.Parse(userID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("invalid user id in token", api.CodeUnauthorized))
			return
		}

		c.Set(string(common.ContextKeyUserID), parsedUserID)
		c.Set(string(common.ContextKeyJTI), jti)
		c.Next()
	}
}
