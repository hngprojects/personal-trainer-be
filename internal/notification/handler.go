package notification

import (
	"errors"
	"log/slog"
	"net/http"

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
	notification, err := h.service.SendNotificationToUser(c.Request.Context(), userID, req.Title, req.Message)
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

	notifications, err := h.service.GetUserNotification(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, api.NewError("notifications not found", api.CodeNotFound))
			return
		}
		h.log.Error("Failed to get user notifications", "error", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to get notifications", api.CodeServerError))
		return
	}
	c.JSON(http.StatusOK, api.NewSuccess("notifications retrieved successfully", api.CodeOK, notifications))
}
