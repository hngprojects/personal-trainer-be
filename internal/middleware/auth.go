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
)

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized,
				api.NewError("Unauthorized; Missing token", api.CodeUnauthorized))
			return
		}

		parts := strings.Split(header, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized,
				api.NewError("Unauthorized; Invalid token", api.CodeUnauthorized))
			return
		}

		token, err := auth.ValidateToken(parts[1])
		if err != nil || !token.Valid {
			log.Println("validate token err: ", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized,
				api.NewError("Unauthorized; Invalid token", api.CodeUnauthorized))
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized,
				api.NewError("Unauthorized; Invalid token", api.CodeUnauthorized))
			return
		}
		sub, _ := claims["sub"].(string)
		uid, err := uuid.Parse(sub)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized,
				api.NewError("Unauthorized; Invalid token", api.CodeUnauthorized))
			return
		}
		c.Set("user_id", uid)
		c.Next()
	}
}

func RequireRole(users auth.UserRepository, role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, ok := c.Get("user_id")
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized,
				api.NewError("unauthorized", api.CodeUnauthorized))
			return
		}
		roles, err := users.ListRoleNames(c.Request.Context(), uid.(uuid.UUID))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError,
				api.NewError("failed to load roles", api.CodeServerError))
			return
		}
		for _, r := range roles {
			if r == role {
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(http.StatusForbidden,
			api.NewError("forbidden", api.CodeForbidden))
	}
}
