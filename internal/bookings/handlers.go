package bookings

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/redis"
)

var (
	defaultBookingStatus   = "pending"
	defaultSessionPlatform = "zoom"
)

type bookingSlotHandler struct {
	service BookingSlotService
	redis   redis.Client
	log     *slog.Logger
}

type BookingSlotHandler interface {
	HandleGetTrainersBookingSlots(c *gin.Context, trainerId uuid.UUID)
}

func NewBookingSlotHandler(service BookingSlotService, redis redis.Client, log *slog.Logger) *bookingSlotHandler {
	return &bookingSlotHandler{service: service, redis: redis, log: log}
}

type bookingHandler struct {
	service BookingService
	redis   redis.Client
	log     *slog.Logger
}

type BookingHandler interface {
	HandleCreateBookingSession(c *gin.Context)
}

func NewBookingHandler(service BookingService, redis redis.Client, log *slog.Logger) *bookingHandler {
	return &bookingHandler{service: service, redis: redis, log: log}
}

func (h *bookingSlotHandler) HandleGetTrainersBookingSlots(c *gin.Context, trainerId uuid.UUID) {
	bookingSlot, err := h.service.GetTrainersBookingSlots(c.Request.Context(), trainerId)
	if err != nil {
		h.log.Error("could not fetch booking slot for trainer", "err", err)
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrTrainerNotFound) {
			c.JSON(http.StatusNotFound, api.ErrorResponse{Code: api.CodeNotFound, Message: err.Error(), Status: "error"})
			return
		}
		c.JSON(http.StatusInternalServerError, api.ErrorResponse{Code: api.CodeServerError, Message: "internal server error", Status: "error"})
		return
	}
	if bookingSlot == nil {
		bookingSlot = []db.GetTrainersBookingSlotsRow{}
	}
	bookingSlotResponse := map[string]interface{}{"data": bookingSlot}
	var data interface{} = bookingSlotResponse
	c.JSON(http.StatusOK, api.SuccessResponse{Code: api.CodeOK, Message: "trainer booking slots retrieved successfully", Data: &data, Meta: nil, Status: "success"})
}

func (h *bookingHandler) HandleCreateBookingSession(c *gin.Context) {
	var request api.CreateBookingJSONBody
	if err := c.ShouldBindJSON(&request); err != nil {
		h.log.Error("error binding request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError(api.CodeBadRequest, "invalid request"))
		return
	}
	// if trainer is not provided
	var fieldErrors []api.FieldError
	if !common.IsNotEmpty(request.TrainerId.String()) {
		h.log.Error("trainer id is required")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "trainers", Message: "please provide a trainer to be booked"})
	}
	if !common.IsNotEmpty(request.Timezone) {
		h.log.Error("timezone is required")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "timezone", Message: "please provide current timezone"})
	}
	if !common.IsNotEmpty(request.SubscriptionId.String()) {
		h.log.Error("subscription id is required")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "subscription", Message: "subscription id is required"})
	}
	// check if subscription is active
	isActive, err := h.service.CheckActiveSubscriptions(c.Request.Context(), request.SubscriptionId)
	if err != nil {
		h.log.Error("failed to check subscription", "error", err)
		fieldErrors = append(fieldErrors, api.FieldError{Field: "subscription", Message: "failed to check subscription"})
	}
	if !isActive {
		h.log.Error("subscription is not active")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "subscription", Message: "subscription is not active"})
	}
	// Check if booking slot is available
	if !common.IsNotEmpty(request.BookingSlot.String()) {
		h.log.Error("booking slot id is required")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "bookingSlot", Message: "booking slot is required"})
	}
	if len(fieldErrors) > 0 {
		c.JSON(http.StatusBadRequest, api.NewValidationError(fieldErrors))
		return
	}
	userIDVal, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("invalid user id", api.CodeUnauthorized))
		return
	}
	data := &db.CreateBookingParams{
		TrainerID:       request.TrainerId,
		ClientID:        userID,
		SubscriptionID:  request.SubscriptionId,
		BookingSlot:     request.BookingSlot,
		BookingStatus:   defaultBookingStatus,
		SessionPlatform: defaultSessionPlatform,
		Timezone:        sql.NullString{Valid: true, String: request.Timezone},
	}

	created, err := h.service.CreateBooking(c.Request.Context(), *data)
	if err != nil {
		h.log.Error("failed to create booking session", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError(api.CodeServerError, "failed to create booking session"))
		return
	}
	var dataInterface interface{} = parseResponse(*created, userID)
	c.JSON(200, dataInterface)
}

func parseResponse(data db.Booking, userID uuid.UUID) api.SuccessResponse {
	response := &db.CreateBookingParams{
		TrainerID:       data.TrainerID,
		ClientID:        userID,
		SubscriptionID:  data.SubscriptionID,
		BookingSlot:     data.BookingSlot,
		BookingStatus:   defaultBookingStatus,
		SessionPlatform: defaultSessionPlatform,
	}
	if data.Timezone.Valid {
		response.Timezone = data.Timezone
	}
	if data.CancellationReason.Valid {
		response.CancellationReason = data.CancellationReason
	}
	if data.CreatedAt.Valid {
		response.CreatedAt = data.CreatedAt
	}
	if data.CancelledAt.Valid {
		response.CancelledAt = data.CancelledAt
	}
	var responseInterface interface{} = response
	return api.SuccessResponse{Code: api.CodeOK, Message: "Booking session created successfully", Data: &responseInterface, Meta: nil, Status: "success"}
}
