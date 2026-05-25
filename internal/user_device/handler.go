package userdevice

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type UserDeviceHandler struct {
	service *userDeviceService
	log     *slog.Logger
}

type UserDeviceHandlerInterface interface {
	HandleRegisterDevice(c *gin.Context)
}

func NewUserDeviceHandler(service *userDeviceService, log *slog.Logger) *UserDeviceHandler {
	return &UserDeviceHandler{
		service: service,
		log:     log,
	}
}

func (h *UserDeviceHandler) HandleRegisterDevice(c *gin.Context) {
	var req api.HandleRegisterDeviceJSONBody
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Error binding JSON", "error", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}
	userIDVal, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		h.log.Error("HandleRegisterDevice: Error retrieving user ID from context")
		c.JSON(http.StatusUnauthorized, api.NewError("unauthorized", api.CodeUnauthorized))
		return
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		h.log.Error("HandleRegisterDevice: Error converting user ID to UUID")
		c.JSON(http.StatusUnauthorized, api.NewError("unauthorized", api.CodeUnauthorized))
		return
	}
	var args = &db.CreateUserDeviceParams{
		UserID:      userID,
		DeviceToken: req.DeviceToken,
		Platform:    string(req.Platform),
	}
	userDevice, err := h.service.RegisterDevice(c.Request.Context(), args.UserID, args.DeviceToken, args.Platform)
	if err != nil {
		h.log.Error("Error registering device", "error", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to register device", api.CodeServerError))
		return
	}
	c.JSON(http.StatusOK, api.NewSuccess("user device successfully registered", api.CodeCreated, userDevice))
}
