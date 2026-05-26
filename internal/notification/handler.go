package notification

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
)

type NotificationHandler struct {
	service *NotificationService
	log     *slog.Logger
}

func NewNotificationHandler(service *NotificationService, log *slog.Logger) *NotificationHandler {
	return &NotificationHandler{
		service: service,
		log:     log,
	}
}

var (
	ErrNotFound = errors.New("not found")
)

type NotificationHandlerInterface interface {
	SendNotificationToUser(c *gin.Context)
	GetUserNotifications(c *gin.Context)
}

func (h *NotificationHandler) SendNotificationToUser(c *gin.Context) {
	var req api.HandleSendNotificationJSONBody
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		h.log.Warn("Validation error: No notification title provided")
		c.JSON(http.StatusBadRequest, api.NewError("notification title is required", api.CodeBadRequest))
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		h.log.Warn("Validation error: No notification message provided")
		c.JSON(http.StatusBadRequest, api.NewError("notification message is required", api.CodeBadRequest))
		return
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		h.log.Warn("Validation error: No notification idempotency key provided")
		c.JSON(http.StatusBadRequest, api.NewError("notification idempotency key is required", api.CodeBadRequest))
		return
	}
	userIDVal, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		h.log.Error("SendNotificationToUser-handler: User ID not found in context")
		c.JSON(http.StatusUnauthorized, api.NewError("user not authenticated", api.CodeUnauthorized))
		return
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		h.log.Error("SendNotificationToUser-handler: Invalid User ID in context")
		c.JSON(http.StatusUnauthorized, api.NewError("user not authenticated", api.CodeUnauthorized))
		return
	}
	// Send notification to user
	notification, err := h.service.SendNotificationToUser(c.Request.Context(), userID, req.Title, req.Message, req.IdempotencyKey)
	if err != nil {
		h.log.Error("Failed to send notification to user", "error", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to send notification", api.CodeServerError))
		return
	}
	c.JSON(http.StatusCreated, api.NewSuccess("notification sent successfully", api.CodeCreated, notification))
}

func (h *NotificationHandler) GetUserNotifications(c *gin.Context) {
	userIDVal, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		h.log.Error("GetUserNotifications-handler: User ID not found in context")
		c.JSON(http.StatusUnauthorized, api.NewError("user not authenticated", api.CodeUnauthorized))
		return
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		h.log.Error("GetUserNotifications-handler: Invalid User ID in context")
		c.JSON(http.StatusUnauthorized, api.NewError("user not authenticated", api.CodeUnauthorized))
		return
	}

	notifications, _ := h.service.GetUserNotification(c.Request.Context(), userID)
	if len(*notifications) < 1 {
		h.log.Error("could not fetch user notifications", "userID", userID)
	}
	c.JSON(http.StatusOK, api.NewSuccess("notifications retrieved successfully", api.CodeOK, notifications))
}
