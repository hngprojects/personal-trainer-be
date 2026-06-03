package middleware

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

// DeactivatedMiddleware blocks requests from users who have deactivated their
// account. Must run after AuthMiddleware (needs the userID in context).
// Exempt routes (e.g. POST /users/me/reactivate) should skip this middleware.
func DeactivatedMiddleware(q *db.Queries, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		val, ok := c.Get(string(common.ContextKeyUserID))
		if !ok {
			c.Next()
			return
		}
		userID, ok := val.(uuid.UUID)
		if !ok {
			c.Next()
			return
		}

		user, err := q.GetUserByID(c.Request.Context(), userID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, api.NewError("user not found", api.CodeUnauthorized))
				return
			}
			log.Error("DeactivatedMiddleware: db lookup failed", "err", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
			return
		}

		if !user.IsActive {
			c.AbortWithStatusJSON(http.StatusForbidden, api.NewError(
				"your account has been deactivated. Visit the app to reactivate.",
				api.CodeForbidden,
			))
			return
		}

		c.Next()
	}
}
