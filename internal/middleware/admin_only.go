package middleware

import (
	"database/sql"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

// adminReadablePaths lists the /admin/* endpoints any admin role can read
// (not just super_admin). Currently the cross-tenant listing dashboards:
// these are read-only views the customer-care / ops admin needs day to
// day, so requiring super_admin would force every read through the
// founders. Mutating /admin routes (AdminAdd, ApproveTrainer, etc.) stay
// super_admin-only because they grant or remove privileges.
var adminReadablePaths = map[string]bool{
	"/api/v1/admin/sessions":            true,
	"/api/v1/admin/discovery-bookings":  true,
	"/api/v1/admin/activities":          true,
	"/api/v1/admin/user/trainer/count":  true,
	"/api/v1/admin/subscriptions/count": true,
	"/api/v1/admin/revenue":             true,
	"/api/v1/admin/clients":             true,
	"/api/v1/admin/top-trainers":        true,
	// Read-only settings dashboard; admins (not just super_admin) need
	// to see current values so customer-care can answer "why is the
	// default session 60 min" without paging the founders. Mutating
	// settings (PUT /admin/settings, POST/DELETE /admin/categories)
	// is NOT in the allowlist — those still require super_admin.
	"/api/v1/admin/settings": true,
}

// SuperAdminOnly protects /api/v1/admin/* routes. Mirrors the path-prefix
// pattern of TrainersAdminOnly so the gating logic stays close to the
// generated routing table without splitting handler groups.
//
// Distinction from TrainersAdminOnly:
//   - this middleware requires role == "super_admin" (TrainersAdminOnly accepts "admin")
//   - plain admin is permitted on GET requests to adminReadablePaths (ops dashboards);
//     all other /admin routes require super_admin
func SuperAdminOnly(q *db.Queries, log *slog.Logger) api.MiddlewareFunc {
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
			log.Warn("super admin middleware: missing authorization header")
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Missing token", api.CodeUnauthorized))
			return
		}

		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) {
			log.Warn("super admin middleware: invalid authorization header format")
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Invalid token", api.CodeUnauthorized))
			return
		}
		tokenString := strings.TrimSpace(strings.TrimPrefix(header, prefix))
		if tokenString == "" {
			log.Warn("super admin middleware: empty token string")
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Invalid token", api.CodeUnauthorized))
			return
		}

		token, err := auth.ValidateAccessToken(tokenString)
		if err != nil || !token.Valid {
			log.Warn("super admin middleware: token validation failed", "err", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Invalid token", api.CodeUnauthorized))
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			log.Warn("super admin middleware: invalid token claims")
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Invalid token claims", api.CodeUnauthorized))
			return
		}
		sub, ok := claims["sub"].(string)
		if !ok || sub == "" {
			log.Warn("super admin middleware: missing subject claim")
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Missing subject claim", api.CodeUnauthorized))
			return
		}
		userID, err := uuid.Parse(sub)
		if err != nil {
			log.Warn("super admin middleware: invalid subject UUID", "sub", sub, "err", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; Invalid subject", api.CodeUnauthorized))
			return
		}

		role, err := q.GetUserRoleByID(c.Request.Context(), userID)
		if err != nil {
			if err == sql.ErrNoRows {
				log.Warn("super admin middleware: user not found", "userID", userID.String(), "err", err)
				c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("Unauthorized; User not found", api.CodeUnauthorized))
				return
			}
			log.Warn("super admin middleware: failed to look up user role", "userID", userID.String(), "err", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
			return
		}

		if role != "super_admin" {
			// Permit plain admins on the read-only listing endpoints; deny
			// every other /admin/* route (those mutate privileges).
			if role != "admin" || c.Request.Method != http.MethodGet || !adminReadablePaths[path] {
				log.Warn("super admin middleware: insufficient role", "userID", userID.String(), "role", role)
				c.AbortWithStatusJSON(http.StatusForbidden, api.NewError("Forbidden; super_admin access required", api.CodeForbidden))
				return
			}
		}

		c.Next()
	}
}
