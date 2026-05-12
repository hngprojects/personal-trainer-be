package middleware

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

// SuperAdminOnly protects /api/v1/admin/* routes. Mirrors the path-prefix
// pattern of TrainersAdminOnly so the gating logic stays close to the
// generated routing table without splitting handler groups.
//
// Distinction from TrainersAdminOnly:
//   - this middleware requires role == "super_admin" (TrainersAdminOnly accepts "admin")
//   - GET requests are NOT exempted — every method requires super_admin here
//     because there are no public read endpoints under /admin
func SuperAdminOnly(q *db.Queries) api.MiddlewareFunc {
	return func(c *gin.Context) {
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		if !strings.HasPrefix(path, "/api/v1/admin") {
			c.Next()
			return
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

		token, err := auth.ValidateAccessToken(tokenString)
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Invalid token", api.CodeUnauthorized))
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

		if role != "super_admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, api.NewError("Forbidden; super_admin access required", api.CodeForbidden))
			return
		}

		c.Next()
	}
}
