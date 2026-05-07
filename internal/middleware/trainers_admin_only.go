// middleware/trainers_admin_only.go
package middleware

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

// TrainersAdminOnly protects /api/v1/trainers* routes.
// It does not affect other OpenAPI endpoints (auth, health, root).
func TrainersAdminOnly(q *db.Queries) gin.HandlerFunc {
	return func(c *gin.Context) {
		// FullPath() is best when available, but during some cases it can be empty.
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		// Only guard trainer endpoints.
		if !strings.HasPrefix(path, "/api/v1/trainers") {
			c.Next()
			return
		}

		// Validate bearer token (reuse internal/auth)
		header := c.GetHeader("Authorization")
		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Missing token", api.CodeUnauthorized))
			return
		}

		parts := strings.Split(header, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Invalid token", api.CodeUnauthorized))
			return
		}

		token, err := auth.ValidateToken(parts[1])
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Invalid token", api.CodeUnauthorized))
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Invalid token claims", api.CodeUnauthorized))
			return
		}

		sub, _ := claims["sub"].(string)
		userID, err := uuid.Parse(sub)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Invalid subject", api.CodeUnauthorized))
			return
		}

		role, err := q.GetUserRoleByID(c.Request.Context(), userID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; User not found", api.CodeUnauthorized))
				return
			}
			c.AbortWithStatusJSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
			return
		}

		if role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, api.NewError("Forbidden; Admin access required", api.CodeForbidden))
			return
		}

		c.Next()
	}
}
