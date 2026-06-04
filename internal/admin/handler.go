// Package admin implements the super_admin-only endpoint for creating
// new admin accounts (POST /admin/add).
package admin

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	errs "github.com/hngprojects/personal-trainer-be/pkg/errors"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
)

// generatedPasswordLen is the length of the password we generate for new
// admins. 16 chars from the friendly password charset is comfortably above
// any practical brute-force threshold against bcrypt.
const generatedPasswordLen = 16

type Handler struct {
	users  auth.AdminUserRepository
	mailer email.Mailer
	log    *slog.Logger
}

func NewHandler(users auth.AdminUserRepository, mailer email.Mailer, log *slog.Logger) *Handler {
	return &Handler{users: users, mailer: mailer, log: log}
}

// AdminAdd handles POST /admin/add. It generates a password, upserts the
// user as an admin with that password, and emails the credentials. The
// plaintext password is never persisted and is sent in email exactly once.
func (h *Handler) AdminAdd(c *gin.Context) {
	var req api.AdminAddJSONRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Warn("admin add: invalid request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	emailAddr := strings.ToLower(strings.TrimSpace(string(req.Email)))
	name := strings.TrimSpace(req.Name)

	if len(emailAddr) > 255 {
		h.log.Warn("admin add: email exceeds 255 characters", "email_len", len(emailAddr))
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "email", Message: "email must not exceed 255 characters"},
		}))
		return
	}
	if !common.IsValidEmail(emailAddr) {
		h.log.Warn("admin add: invalid email format", "email_len", len(emailAddr))
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "email", Message: "invalid email format"},
		}))
		return
	}
	if name == "" {
		h.log.Warn("admin add: name is empty")
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

	user, err := h.users.UpsertAdminUser(c.Request.Context(), emailAddr, name, hash)
	if err != nil {
		if errors.Is(err, errs.ErrConflict) {
			h.log.Warn("admin add: admin already exists", "email_len", len(emailAddr))
			c.JSON(http.StatusConflict, api.NewError("admin with this email already exists", api.CodeConflict))
			return
		}
		h.log.Error("admin add: upsert admin failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	if user == nil {
		h.log.Warn("admin add: upsert returned nil user (conflict)", "email_len", len(emailAddr))
		c.JSON(http.StatusConflict, api.NewError("admin with this email already exists", api.CodeConflict))
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
