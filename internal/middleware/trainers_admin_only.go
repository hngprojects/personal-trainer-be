package middleware

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

// TrainersAdminOnly protects /api/v1/trainers* routes.
// - Public: /api/v1/trainers/login, /api/v1/trainers/setup-password
// - GET: any authenticated user
// - Non-GET: admin only
func TrainersAdminOnly(q *db.Queries) api.MiddlewareFunc {
	return func(c *gin.Context) {
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		if !strings.HasPrefix(path, "/api/v1/trainers") {
			c.Next()
			return
		}

		// Public trainer endpoints (no JWT)
		switch path {
		case "/api/v1/trainers/login", "/api/v1/trainers/setup-password":
			c.Next()
			return
		}

		// Mock auth support (unchanged behavior)
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
				if c.Request.Method != http.MethodGet && mockRole != "admin" {
					c.AbortWithStatusJSON(http.StatusForbidden, api.NewError("Forbidden; Admin access required", api.CodeForbidden))
					return
				}
				c.Next()
				return
			}
		}

		// At this point, the router's AuthMiddleware should already have run for secured endpoints
		// and stored the user ID in context.
		v, ok := c.Get(string(common.ContextKeyUserID))
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Missing token", api.CodeUnauthorized))
			return
		}
		userID, ok := v.(uuid.UUID)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Invalid token", api.CodeUnauthorized))
			return
		}

		// GET: allow any authenticated user
		if c.Request.Method == http.MethodGet {
			c.Next()
			return
		}

		// Non-GET: must be admin
		hasAdmin, err := q.UserHasRole(c.Request.Context(), db.UserHasRoleParams{
			UserID: userID,
			Name:   "admin",
		})
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
			return
		}
		if !hasAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, api.NewError("Forbidden; Admin access required", api.CodeForbidden))
			return
		}

		c.Next()
	}
}