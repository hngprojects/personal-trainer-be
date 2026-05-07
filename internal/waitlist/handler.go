package waitlist

import (
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

// HandleAddWaitlist handles POST /waitlist
func (h *WaitlistHandler) HandleAddWaitlist(c *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Feedback string `json:"feedback"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Warn("invalid request for add waitlist", "err", err)
		c.JSON(http.StatusBadRequest, api.NewErrorResponse("email field is required", api.CodeBadRequest, nil))
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))

	if err := h.repo.AddEmail(c.Request.Context(), email, req.Feedback); err != nil {
		h.log.Error("failed to add email to waitlist", "err", err, "email", email)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	h.log.Info("email added to waitlist", "email", email)
	c.JSON(http.StatusOK, api.NewSuccessResponse("successfully added to the waitlist", api.CodeOK, nil, nil))
}

// HandleGetWaitlist handles GET /waitlist
func (h *WaitlistHandler) HandleGetWaitlist(c *gin.Context, params api.HandleGetWaitlistParams) {
	email := ""
	if params.Email != nil {
		email = strings.ToLower(strings.TrimSpace(*params.Email))
	}

	if email != "" {
		// Get specific email
		result, err := h.repo.GetByEmail(c.Request.Context(), email)
		if err != nil {
			if err == ErrNotFound {
				h.log.Warn("email not found in waitlist", "email", email)
				c.JSON(http.StatusNotFound, api.NewErrorResponse("requested email is not on the waitlist", api.CodeNotFound, nil))
				return
			}
			h.log.Error("failed to get waitlist by email", "err", err, "email", email)
			c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
			return
		}
		data := map[string]interface{}{
			"id":         result.ID,
			"email":      result.Email,
			"feedback":   result.Feedback,
			"created_at": result.CreatedAt,
		}

		c.JSON(http.StatusOK, api.NewSuccess("success", api.CodeOK, data))
		return
	}

	// Get all waitlist entries
	results, err := h.repo.GetAll(c.Request.Context())
	if err != nil {
		h.log.Error("failed to get waitlist", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	items := make([]map[string]interface{}, 0, len(results))

	for _, r := range results {
		items = append(items, map[string]interface{}{
			"id":         r.ID,
			"email":      r.Email,
			"feedback":   r.Feedback,
			"created_at": r.CreatedAt,
		})
	}

	data := map[string]interface{}{
		"items": items,
	}

	c.JSON(http.StatusOK, api.NewSuccess("success", api.CodeOK, data))
}
