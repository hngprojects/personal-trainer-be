package middleware

import (
	"database/sql"
	"net/http"
	"os"
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
func TrainersAdminOnly(q *db.Queries) api.MiddlewareFunc {
	return func(c *gin.Context) {
		// FullPath() is preferred, but can be empty depending on when middleware runs.
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		// Only guard trainers endpoints.
		if !strings.HasPrefix(path, "/api/v1/trainers") {
			c.Next()
			return
		}

		// Public trainer review listing remains outside the trainer auth/admin guard.
		if c.Request.Method == http.MethodGet &&
			strings.HasPrefix(path, "/api/v1/trainers/") &&
			strings.HasSuffix(path, "/reviews") {
			c.Next()
			return
		}

		// Trainer-owned endpoints (/trainers/me/*) are accessible to any authenticated trainer.
		if strings.HasPrefix(path, "/api/v1/trainers/me/") || strings.HasPrefix(path, "/trainers/me/") {
			c.Next()
			return
		}

		if os.Getenv("ENABLE_MOCK_AUTH") == "1" && (os.Getenv("ENV") == "test" || os.Getenv("ENV") == "development") {
			mockRole := strings.TrimSpace(c.GetHeader("X-Mock-Role"))
			mockID := strings.TrimSpace(c.GetHeader("X-Mock-User-ID"))
			if mockID != "" {
				if _, err := uuid.Parse(mockID); err != nil {
					c.AbortWithStatusJSON(http.StatusBadRequest, api.NewError("Invalid X-Mock-User-ID header; must be a valid UUID", api.CodeBadRequest))
					return
				}
			}
			if mockRole != "" {
				if mockRole != "admin" {
					c.AbortWithStatusJSON(http.StatusForbidden, api.NewError("Forbidden; Admin access required", api.CodeForbidden))
					return
				}
				c.Next()
				return
			}
		}

		header := c.GetHeader("Authorization")
		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Missing token", api.CodeUnauthorized))
			return
		}

		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Invalid token", api.CodeUnauthorized))
			return
		}
		tokenString := strings.TrimSpace(strings.TrimPrefix(header, prefix))
		if tokenString == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Invalid token", api.CodeUnauthorized))
			return
		}

		token, err := auth.ValidateToken(tokenString)
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Invalid token", api.CodeUnauthorized))
			return
		}

		// GET: any authenticated user (valid JWT) can access trainers endpoints.
		// No admin role check for GET.
		if c.Request.Method == http.MethodGet {
			c.Next()
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Invalid token claims", api.CodeUnauthorized))
			return
		}

		sub, ok := claims["sub"].(string)
		if !ok || sub == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Missing subject claim", api.CodeUnauthorized))
			return
		}

		userID, err := uuid.Parse(sub)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Invalid subject", api.CodeUnauthorized))
			return
		}

		role, err := q.GetUserRoleByID(c.Request.Context(), userID)
		if err != nil {
			if err == sql.ErrNoRows {
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
