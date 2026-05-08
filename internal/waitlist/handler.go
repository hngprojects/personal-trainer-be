package waitlist

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
)

type WaitlistHandler struct {
	repo WaitlistRepository
	log  *slog.Logger
}

func NewWaitlistHandler(repo WaitlistRepository, log *slog.Logger) *WaitlistHandler {
	return &WaitlistHandler{
		repo: repo,
		log:  log,
	}
}

func (h *WaitlistHandler) HandleAddWaitlist(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Warn("invalid request for add waitlist", "err", err)
		c.JSON(http.StatusBadRequest, api.NewErrorResponse("Invalid or missing email", api.CodeBadRequest, nil))
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))

	// Check if email already exists
	_, err := h.repo.GetByEmail(c.Request.Context(), email)
	if err == nil {
		// Email already exists, return 200 OK
		h.log.Info("email already on waitlist", "email", email)
		c.JSON(http.StatusOK, api.NewSuccessResponse("You're already on the waitlist", api.CodeOK, nil, nil))
		return
	}
	if !errors.Is(err, ErrNotFound) {
		// Unexpected error
		h.log.Error("failed to check if email exists", "err", err, "email", email)
		c.JSON(http.StatusInternalServerError, api.NewError("Internal server error", api.CodeServerError))
		return
	}

	// Email doesn't exist, add it
	if err := h.repo.AddEmail(c.Request.Context(), email); err != nil {
		h.log.Error("failed to add email to waitlist", "err", err, "email", email)
		c.JSON(http.StatusInternalServerError, api.NewError("Internal server error", api.CodeServerError))
		return
	}

	h.log.Info("email added to waitlist", "email", email)
	c.JSON(http.StatusCreated, api.NewSuccessResponse("Successfully added to the waitlist", api.CodeCreated, nil, nil))
}

func (h *WaitlistHandler) HandleGetWaitlist(c *gin.Context, params api.HandleGetWaitlistParams) {
	email := ""
	if params.Email != nil {
		email = strings.ToLower(strings.TrimSpace(*params.Email))
	}

	if email != "" {
		result, err := h.repo.GetByEmail(c.Request.Context(), email)
		if err != nil {
			if err == ErrNotFound {
				h.log.Warn("email not found in waitlist", "email", email)
				c.JSON(http.StatusNotFound, api.NewErrorResponse("Requested email is not on the waitlist", api.CodeNotFound, nil))
				return
			}
			h.log.Error("failed to get waitlist by email", "err", err, "email", email)
			c.JSON(http.StatusInternalServerError, api.NewError("Internal server error", api.CodeServerError))
			return
		}
		data := map[string]interface{}{
			"id":         result.ID,
			"email":      result.Email,
			"created_at": result.CreatedAt,
		}

		c.JSON(http.StatusOK, api.NewSuccess("success", api.CodeOK, data))
		return
	}

	results, err := h.repo.GetAll(c.Request.Context())
	if err != nil {
		h.log.Error("failed to get waitlist", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("Internal server error", api.CodeServerError))
		return
	}
	items := make([]map[string]interface{}, 0, len(results))

	for _, r := range results {
		items = append(items, map[string]interface{}{
			"id":         r.ID,
			"email":      r.Email,
			"created_at": r.CreatedAt,
		})
	}

	data := map[string]interface{}{
		"items": items,
	}

	c.JSON(http.StatusOK, api.NewSuccess("Success", api.CodeOK, data))
}
