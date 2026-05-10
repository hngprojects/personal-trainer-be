// Package admin implements the super_admin-only endpoints for managing
// admin accounts: creating new admins (POST /admin/add) and updating the
// role of an existing admin/super_admin (PUT /admin/{id}/role).
package admin

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
)

// generatedPasswordLen is the length of the password we generate for new
// admins. 16 chars from the friendly password charset is comfortably above
// any practical brute-force threshold against bcrypt.
const generatedPasswordLen = 16

// validRoles is the closed set of role values AdminUpdateRole will accept.
// Promotion to/from these roles is the only operation /admin/{id}/role
// supports — clients/trainers are not in scope here.
var validRoles = map[string]struct{}{
	"admin":       {},
	"super_admin": {},
}

type Handler struct {
	users  auth.UserRepository
	mailer email.Mailer
	log    *slog.Logger
}

func NewHandler(users auth.UserRepository, mailer email.Mailer, log *slog.Logger) *Handler {
	return &Handler{users: users, mailer: mailer, log: log}
}

// AdminAdd handles POST /admin/add. It generates a password, upserts the
// user as an admin with that password, and emails the credentials. The
// plaintext password is never persisted and is sent in email exactly once.
func (h *Handler) AdminAdd(c *gin.Context) {
	var req api.AdminAddJSONRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	emailAddr := strings.ToLower(strings.TrimSpace(string(req.Email)))
	name := strings.TrimSpace(req.Name)

	if len(emailAddr) > 255 {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "email", Message: "email must not exceed 255 characters"},
		}))
		return
	}
	if !common.IsValidEmail(emailAddr) {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "email", Message: "invalid email format"},
		}))
		return
	}
	if name == "" {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "name", Message: "name is required"},
		}))
		return
	}

	password, err := auth.GenerateRandomPassword(generatedPasswordLen)
	if err != nil {
		h.log.Error("admin add: generate password failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		h.log.Error("admin add: hash password failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	user, err := h.users.UpsertAdmin(c.Request.Context(), emailAddr, name, hash)
	if err != nil {
		h.log.Error("admin add: upsert admin failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	if err := h.mailer.SendAdminCredentials(emailAddr, password); err != nil {
		// Row was already written. Surfacing 500 lets the super_admin retry;
		// UpsertAdmin is idempotent for the same (email, local) pair so a retry
		// rotates the password rather than creating a duplicate.
		h.log.Error("admin add: send credentials email failed", "err", err, "user_id", user.ID)
		c.JSON(http.StatusInternalServerError, api.NewError("admin created but email failed; please retry", api.CodeServerError))
		return
	}

	h.log.Info("admin created", "user_id", user.ID)

	c.JSON(http.StatusCreated, api.NewSuccess("admin account created and credentials emailed", api.CodeCreated, map[string]interface{}{
		"id":    user.ID,
		"email": user.Email,
		"name":  user.Name,
		"role":  user.Role,
	}))
}

// AdminUpdateRole handles PUT /admin/{id}/role. Only valid for users who are
// already admin or super_admin — promoting clients/trainers via this endpoint
// is intentionally not supported.
func (h *Handler) AdminUpdateRole(c *gin.Context, id uuid.UUID) {
	var req api.AdminUpdateRoleJSONRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	role := strings.TrimSpace(string(req.Role))
	if _, ok := validRoles[role]; !ok {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "role", Message: "role must be 'admin' or 'super_admin'"},
		}))
		return
	}

	target, err := h.users.GetByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			c.JSON(http.StatusNotFound, api.NewError("user not found", api.CodeNotFound))
			return
		}
		h.log.Error("admin update role: lookup failed", "err", err, "user_id", id)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	if _, ok := validRoles[target.Role]; !ok {
		c.JSON(http.StatusForbidden, api.NewError("target user is not an admin or super_admin", api.CodeForbidden))
		return
	}

	updated, err := h.users.UpdateRole(c.Request.Context(), id, role)
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			c.JSON(http.StatusNotFound, api.NewError("user not found", api.CodeNotFound))
			return
		}
		h.log.Error("admin update role: update failed", "err", err, "user_id", id)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	h.log.Info("admin role updated", "user_id", updated.ID, "new_role", updated.Role)

	c.JSON(http.StatusOK, api.NewSuccess("role updated", api.CodeOK, map[string]interface{}{
		"id":    updated.ID,
		"email": updated.Email,
		"name":  updated.Name,
		"role":  updated.Role,
	}))
}
