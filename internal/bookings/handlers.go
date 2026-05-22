package bookings

import (
	"database/sql"
	"encoding/json"
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
	defaultBookingStatus = "pending"
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

const bookingSlotsCacheTTL = 15 * time.Minute

func (h *bookingSlotHandler) HandleGetTrainersBookingSlots(c *gin.Context, trainerId uuid.UUID) {
	ctx := c.Request.Context()
	cacheKey := "booking-slots:trainer:" + trainerId.String()

	// Cache read — skip on Redis error, proceed to DB.
	if cached := h.redis.Get(ctx, cacheKey); cached.Err() == nil {
		var slots []db.GetTrainersBookingSlotsRow
		if err := json.Unmarshal([]byte(cached.Val()), &slots); err == nil {
			h.log.Info("HandleGetTrainersBookingSlots: cache hit", "trainer_id", trainerId)
			bookingSlotResponse := map[string]interface{}{"data": slots}
			var data interface{} = bookingSlotResponse
			c.JSON(http.StatusOK, api.SuccessResponse{Code: api.CodeOK, Message: "trainer booking slots retrieved successfully", Data: &data, Meta: nil, Status: "success"})
			return
		} else {
			h.log.Warn("HandleGetTrainersBookingSlots: failed to unmarshal cached data, falling back to DB", "trainer_id", trainerId, "err", err)
		}
	} else {
		h.log.Warn("HandleGetTrainersBookingSlots: cache miss or error, falling back to DB", "trainer_id", trainerId, "err", cached.Err())
	}

	bookingSlot, err := h.service.GetTrainersBookingSlots(ctx, trainerId)
	if err != nil {
		h.log.Warn("HandleGetTrainersBookingSlots: failed to fetch booking slots", "trainer_id", trainerId, "err", err)
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

	// Cache the result for subsequent reads.
	if raw, err := json.Marshal(bookingSlot); err == nil {
		_ = h.redis.Set(ctx, cacheKey, raw, bookingSlotsCacheTTL)
	}

	bookingSlotResponse := map[string]interface{}{"data": bookingSlot}
	var data interface{} = bookingSlotResponse
	c.JSON(http.StatusOK, api.SuccessResponse{Code: api.CodeOK, Message: "trainer booking slots retrieved successfully", Data: &data, Meta: nil, Status: "success"})
}

func (h *bookingHandler) HandleCreateBookingSession(c *gin.Context) {
	var request api.CreateBookingJSONBody
	if err := c.ShouldBindJSON(&request); err != nil {
		h.log.Warn("error binding request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError(api.CodeBadRequest, "invalid request"))
		return
	}
	// if trainer is not provided
	var fieldErrors []api.FieldError
	if request.TrainerId == uuid.Nil {
		h.log.Warn("trainer id is required")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "trainers", Message: "please provide a trainer to be booked"})
	}
	if !common.IsNotEmpty(request.Timezone) {
		h.log.Warn("timezone is required")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "timezone", Message: "please provide current timezone"})
	}
	// Check if booking slot is available
	if request.ScheduledStart.IsZero() {
		h.log.Warn("select a booking start time")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "bookingSlot", Message: "select a booking start time"})
	}
	if request.ScheduledEnd.IsZero() {
		h.log.Warn("select a booking end time")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "bookingSlot", Message: "select a booking end time"})
	}
	if !request.ScheduledStart.IsZero() && !request.ScheduledEnd.IsZero() && request.ScheduledEnd.Before(request.ScheduledStart) {
		h.log.Warn("booking end time must be after start time")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "bookingSlot", Message: "booking end time must be after start time"})
	}
	if !common.IsNotEmpty(string(request.SessionPlatform)) {
		h.log.Warn("select a session platform")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "sessionPlatform", Message: "select a session platform"})
	}
	if !request.SessionPlatform.Valid() {
		h.log.Warn("select a valid session platform")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "sessionPlatform", Message: "select a valid session platform, ['google meet', 'zoom', 'whatsapp']"})
	}
	if len(fieldErrors) > 0 {
		c.JSON(http.StatusBadRequest, api.NewValidationError(fieldErrors))
		return
	}
	userIDVal, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		h.log.Warn("HandleCreateBookingSession: missing authenticated user in context")
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		h.log.Warn("HandleCreateBookingSession: invalid user id type in context")
		c.JSON(http.StatusUnauthorized, api.NewError("invalid user id", api.CodeUnauthorized))
		return
	}
	data := &db.CreateBookingParams{
		TrainerID:       request.TrainerId,
		ClientID:        userID,
		ScheduledStart:  sql.NullTime{Valid: true, Time: request.ScheduledStart},
		ScheduledEnd:    sql.NullTime{Valid: true, Time: request.ScheduledEnd},
		BookingStatus:   sql.NullString{Valid: true, String: defaultBookingStatus},
		SessionPlatform: sql.NullString{Valid: true, String: string(request.SessionPlatform)},
		Timezone:        sql.NullString{Valid: true, String: request.Timezone},
	}
	userData, err := h.service.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		if err == ErrNotFound {
			h.log.Warn("failed to get user by id", "err", err)
			c.JSON(http.StatusNotFound, api.NewError(api.CodeNotFound, "failed to get user by id"))
		}
		h.log.Warn("Get user by id: an error occured during DB look up", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError(api.CodeServerError, "failed to get user by id"))
		return
	}
	trainer, err := h.service.GetTrainerDetails(c.Request.Context(), request.TrainerId)
	if err != nil {
		if err == ErrNotFound {
			h.log.Warn("failed to get trainer by id", "err", err)
			c.JSON(http.StatusNotFound, api.NewError(api.CodeNotFound, "failed to get trainer by id"))
		}
		h.log.Warn("Get trainer by id: an error occured during DB look up", "err", err)
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
		ScheduledStart     *time.Time `json:"scheduled_start"`
		ScheduledEnd       *time.Time `json:"scheduled_end"`
		Timezone           *string    `json:"timezone"`
		BookingStatus      *string    `json:"booking_status"`
		SessionPlatform    *string    `json:"session_platform"`
		CancellationReason *string    `json:"cancellation_reason"`
		ZoomMeetingLink    *string    `json:"zoom_meeting_link"`
		ZoomMeetingID      *string    `json:"zoom_meeting_id"`
		CreatedAt          *time.Time `json:"created_at"`
		CancelledAt        *time.Time `json:"cancelled_at"`
	}

	response := BookingSessionResponse{
		ID:        data.ID,
		TrainerID: data.TrainerID,
		ClientID:  userID,
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
	if data.ZoomMeetingLink.Valid {
		response.ZoomMeetingLink = &data.ZoomMeetingLink.String
	}
	if data.ZoomMeetingID.Valid {
		response.ZoomMeetingID = &data.ZoomMeetingID.String
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
