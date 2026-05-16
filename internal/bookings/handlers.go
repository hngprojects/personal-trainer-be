package bookings

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

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
	// redis   redis.Client
	log *slog.Logger
}

type BookingHandler interface {
	HandleCreateBookingSession(c *gin.Context)
}

func NewBookingHandler(service BookingService, log *slog.Logger) *bookingHandler {
	return &bookingHandler{service: service, log: log}
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
	// Check if booking slot is available
	if !common.IsNotEmpty(request.ScheduledStart.String()) {
		h.log.Error("select a booking start time")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "bookingSlot", Message: "select a booking start time"})
	}
	if !common.IsNotEmpty(request.ScheduledEnd.String()) {
		h.log.Error("select a booking end time")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "bookingSlot", Message: "select a booking end time"})
	}
	if !common.IsNotEmpty(string(request.SessionPlatform)) {
		h.log.Error("select a session platform")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "sessionPlatform", Message: "select a session platform"})
	}
	if !request.SessionPlatform.Valid() {
		h.log.Error("select a valid session platform")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "sessionPlatform", Message: "select a valid session platform, ['google meet', 'zoom', 'whatsapp']"})
	}
	// check subscription status
	activeSub, err := h.service.CheckSubscription(c.Request.Context(), request.SubscriptionId)
	if err != nil {
		h.log.Error("failed to check subscription", "error", err)
		fieldErrors = append(fieldErrors, api.FieldError{Field: "subscriptionId", Message: "could not get subscription status"})
	}
	if !activeSub {
		h.log.Error("non-active subscription")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "subscriptionId", Message: "subscription is not active"})
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
		SubscriptionID:  uuid.NullUUID{Valid: true, UUID: request.SubscriptionId},
		ScheduledStart:  sql.NullTime{Valid: true, Time: request.ScheduledStart},
		ScheduledEnd:    sql.NullTime{Valid: true, Time: request.ScheduledEnd},
		BookingStatus:   sql.NullString{Valid: true, String: defaultBookingStatus},
		SessionPlatform: sql.NullString{Valid: true, String: defaultSessionPlatform},
		Timezone:        sql.NullString{Valid: true, String: request.Timezone},
	}
	userData, err := h.service.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		h.log.Error("failed to get user by id", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError(api.CodeServerError, "failed to get user by id"))
		return
	}
	trainer, err := h.service.GetTrainerDetails(c.Request.Context(), request.TrainerId)
	if err != nil {
		h.log.Error("failed to get trainer by id", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError(api.CodeServerError, "failed to get trainer by id"))
		return
	}

	created, err := h.service.CreateBooking(c.Request.Context(), *data, *userData, *trainer)
	if err != nil {
		h.log.Error("failed to create booking session", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError(api.CodeServerError, "failed to create booking session"))
		return
	}
	var dataInterface interface{} = parseResponse(*created, userID)
	c.JSON(200, dataInterface)
}

func parseResponse(data db.Booking, userID uuid.UUID) api.SuccessResponse {
	type BookingSessionResponse struct {
		ID                 uuid.UUID  `json:"id"`
		TrainerID          uuid.UUID  `json:"trainer_id"`
		ClientID           uuid.UUID  `json:"client_id"`
		SubscriptionID     uuid.UUID  `json:"subscription_id"`
		ScheduledStart     *time.Time `json:"scheduled_start"`
		ScheduledEnd       *time.Time `json:"scheduled_end"`
		Timezone           *string    `json:"timezone"`
		BookingStatus      *string    `json:"booking_status"`
		SessionPlatform    *string    `json:"session_platform"`
		CancellationReason *string    `json:"cancellation_reason"`
		CreatedAt          *time.Time `json:"created_at"`
		CancelledAt        *time.Time `json:"cancelled_at"`
	}

	response := BookingSessionResponse{
		ID:        data.ID,
		TrainerID: data.TrainerID,
		ClientID:  userID,
	}
	if data.SubscriptionID.Valid {
		response.SubscriptionID = data.SubscriptionID.UUID
	}
	if data.ScheduledStart.Valid {
		response.ScheduledStart = &data.ScheduledStart.Time
	}
	if data.ScheduledEnd.Valid {
		response.ScheduledEnd = &data.ScheduledEnd.Time
	}
	if data.Timezone.Valid {
		response.Timezone = &data.Timezone.String
	}
	if data.BookingStatus.Valid {
		response.BookingStatus = &data.BookingStatus.String
	}
	if data.SessionPlatform.Valid {
		response.SessionPlatform = &data.SessionPlatform.String
	}
	if data.CancellationReason.Valid {
		response.CancellationReason = &data.CancellationReason.String
	}
	if data.CreatedAt.Valid {
		response.CreatedAt = &data.CreatedAt.Time
	}
	if data.CancelledAt.Valid {
		response.CancelledAt = &data.CancelledAt.Time
	}
	var responseInterface interface{} = response
	return api.SuccessResponse{Code: api.CodeOK, Message: "Booking session created successfully", Data: &responseInterface, Meta: nil, Status: "success"}
}
