package contact

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
)

type Handler struct {
	q      *db.Queries
	log    *slog.Logger
	mailer email.Mailer
}

func NewHandler(q *db.Queries, log *slog.Logger, mailer email.Mailer) *Handler {
	return &Handler{q: q, log: log, mailer: mailer}
}

func (h *Handler) HandleContactUs(c *gin.Context) {
	var req api.HandleContactUsJSONRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Warn("invalid contact-us request", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("Invalid or missing parameters", api.CodeBadRequest))
		return
	}

	emailAddr := strings.ToLower(strings.TrimSpace(string(req.Email)))
	name := strings.TrimSpace(req.Name)
	subject := strings.TrimSpace(req.Subject)
	message := strings.TrimSpace(req.Message)

	if !common.IsValidEmail(emailAddr) {
		c.JSON(http.StatusBadRequest, api.NewError("Invalid email address", api.CodeBadRequest))
		return
	}

	if name == "" || subject == "" || message == "" {
		c.JSON(http.StatusBadRequest, api.NewError("Invalid or missing parameters", api.CodeBadRequest))
		return
	}

	if _, err := h.q.CreateMessage(c.Request.Context(), db.CreateMessageParams{
		Email:   emailAddr,
		Name:    name,
		Subject: subject,
		Message: message,
	}); err != nil {
		h.log.Error("failed to save contact message", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("Internal server error", api.CodeServerError))
		return
	}

	if err := h.mailer.SendContactConfirmation(emailAddr, name); err != nil {
		h.log.Error("failed to send contact confirmation email", "email", emailAddr, "err", err)
	}

	c.JSON(http.StatusOK, api.NewSuccess("Your feedback has been received", api.CodeOK, nil))
}
