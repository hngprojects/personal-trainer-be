// Package admin implements the super_admin-only endpoint for creating
// new admin accounts (POST /admin/add).
package admin

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
)

// generatedPasswordLen is the length of the password we generate for new
// admins. 16 chars from the friendly password charset is comfortably above
// any practical brute-force threshold against bcrypt.
const generatedPasswordLen = 16

type Handler struct {
	users  auth.AdminUserRepository
	q      *db.Queries
	mailer email.Mailer
	log    *slog.Logger
}

func NewHandler(users auth.AdminUserRepository, q *db.Queries, mailer email.Mailer, log *slog.Logger) *Handler {
	return &Handler{users: users, q: q, mailer: mailer, log: log}
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

	user, err := h.users.UpsertAdminUser(c.Request.Context(), emailAddr, name, hash)
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

// AdminCreateTrainer handles POST /admin/trainers.
func (h *Handler) AdminCreateTrainer(c *gin.Context) {
	var req struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	emailAddr := strings.ToLower(strings.TrimSpace(req.Email))
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
		h.log.Error("admin create trainer: generate password failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		h.log.Error("admin create trainer: hash password failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	ctx := c.Request.Context()

	// 1) Create/Upsert user (local)
	user, err := h.q.CreateUser(ctx, db.CreateUserParams{
		Email:        emailAddr,
		Name:         name,
		AuthProvider: "local",
	})
	if err != nil {
		h.log.Error("admin create trainer: create user failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	// 2) Set temp password + activate
	if err := h.q.UpdateUserPasswordByID(ctx, db.UpdateUserPasswordByIDParams{
		ID:       user.ID,
		Password: sql.NullString{String: hash, Valid: true},
	}); err != nil {
		h.log.Error("admin create trainer: update password failed", "err", err, "user_id", user.ID)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	// 3) Assign trainer role using roles/user_roles
	role, err := h.q.GetRoleByName(ctx, "trainer")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Should not happen because migration seeds roles; still guard.
			c.JSON(http.StatusServiceUnavailable, api.NewError("trainer role is not configured on this server", api.CodeServerError))
			return
		}
		h.log.Error("admin create trainer: get role failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	if err := h.q.AssignUserRole(ctx, db.AssignUserRoleParams{
		UserID: user.ID,
		RoleID: role.ID,
	}); err != nil {
		h.log.Error("admin create trainer: assign role failed", "err", err, "user_id", user.ID, "role_id", role.ID)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	// 4) Create trainer profile row
	// NOTE: This will error if you already have a trainer row for this user and trainers.user_id is unique.
	trainer, err := h.q.CreateTrainer(ctx, db.CreateTrainerParams{
		UserID:            user.ID,
		Specialization:    sql.NullString{Valid: false},
		Bio:               sql.NullString{Valid: false},
		YearsOfExperience: sql.NullInt32{Valid: false},
		IntroVideoUrl:     sql.NullString{Valid: false},
		DisplayPicture:    sql.NullString{Valid: false},
		CalendlyConnected: false,
		CalendlyLink:      sql.NullString{Valid: false},
		OnboardingStatus:  "pending",
	})
	if err != nil {
		h.log.Error("admin create trainer: create trainer failed", "err", err, "user_id", user.ID)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	// 5) Email credentials
	// If you don't have SendTrainerCredentials yet, you can temporarily call SendAdminCredentials.
	if err := h.mailer.SendTrainerCredentials(emailAddr, password); err != nil {
		h.log.Error("admin create trainer: send credentials email failed", "err", err, "user_id", user.ID)
		c.JSON(http.StatusInternalServerError, api.NewError("trainer created but email failed; please retry", api.CodeServerError))
		return
	}

	h.log.Info("trainer created", "user_id", user.ID, "trainer_id", trainer.ID)

	c.JSON(http.StatusCreated, api.NewSuccess(
		"trainer account created and credentials emailed",
		api.CodeCreated,
		map[string]any{
			"user": map[string]any{
				"id":    user.ID,
				"email": user.Email,
				"name":  user.Name,
			},
			"trainer": map[string]any{
				"id":                 trainer.ID,
				"user_id":            trainer.UserID,
				"calendly_connected": trainer.CalendlyConnected,
				"onboarding_status":  trainer.OnboardingStatus,
				"average_rating":     trainer.AverageRating,
				"total_reviews":      trainer.TotalReviews,
				"created_at":         trainer.CreatedAt,
				"updated_at":         trainer.UpdatedAt,
			},
		},
	))
}
