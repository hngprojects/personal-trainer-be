package bookings

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/internal/notification"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
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
	HandleGetTrainersBookingSlots(c *gin.Context, trainerId uuid.UUID, params api.GetTrainersBookingSlotsParams)
}

func NewBookingSlotHandler(service BookingSlotService, redis redis.Client, log *slog.Logger) *bookingSlotHandler {
	return &bookingSlotHandler{service: service, redis: redis, log: log}
}

type bookingHandler struct {
	service BookingService
	log     *slog.Logger
	notif   *notification.NotificationService
	// redis is optional — when set, a successful CreateBooking
	// invalidates the trainer's cached slot list so the next
	// /booking-slots/{trainerId} response reflects the new booking.
	// Nil-safe; cache invalidation is best-effort.
	redis *redis.Client
}

type BookingHandler interface {
	HandleCreateBookingSession(c *gin.Context)
}

func NewBookingHandler(service BookingService, log *slog.Logger, notif *notification.NotificationService, r *redis.Client) *bookingHandler {
	return &bookingHandler{service: service, log: log, notif: notif, redis: r}
}

const bookingSlotsCacheTTL = 15 * time.Minute

func (h *bookingSlotHandler) HandleGetTrainersBookingSlots(c *gin.Context, trainerId uuid.UUID, params api.GetTrainersBookingSlotsParams) {
	ctx := c.Request.Context()

	// When the caller pins a specific date, skip the cache entirely and
	// always hit the DB. The cache holds the unfiltered template list;
	// caching per-date results would explode the keyspace, and bookings
	// happen frequently enough that cached date-results would go stale
	// faster than the 15-minute TTL anyway. Worst case is one extra DB
	// hit per slot picker open.
	if params.Date != nil {
		targetDate := params.Date.Time
		filtered, err := h.service.GetTrainersBookingSlotsForDate(ctx, trainerId, targetDate)
		if err != nil {
			h.log.Warn("HandleGetTrainersBookingSlots: filtered fetch failed", "trainer_id", trainerId, "date", targetDate, "err", err)
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrTrainerNotFound) {
				c.JSON(http.StatusNotFound, api.ErrorResponse{Code: api.CodeNotFound, Message: err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, api.ErrorResponse{Code: api.CodeServerError, Message: "internal server error"})
			return
		}
		if filtered == nil {
			filtered = []db.GetTrainersBookingSlotsRow{}
		}
		resp := map[string]interface{}{"data": filtered}
		var data interface{} = resp
		c.JSON(http.StatusOK, api.SuccessResponse{Code: api.CodeOK, Message: "trainer booking slots retrieved successfully", Data: &data, Meta: nil})
		return
	}

	cacheKey := "booking-slots:trainer:" + trainerId.String()

	// Cache read — skip on Redis error, proceed to DB.
	if cached := h.redis.Get(ctx, cacheKey); cached.Err() == nil {
		var slots []db.GetTrainersBookingSlotsRow
		if err := json.Unmarshal([]byte(cached.Val()), &slots); err == nil {
			h.log.Info("HandleGetTrainersBookingSlots: cache hit", "trainer_id", trainerId)
			bookingSlotResponse := map[string]interface{}{"data": slots}
			var data interface{} = bookingSlotResponse
			c.JSON(http.StatusOK, api.SuccessResponse{Code: api.CodeOK, Message: "trainer booking slots retrieved successfully", Data: &data, Meta: nil})
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
			c.JSON(http.StatusNotFound, api.ErrorResponse{Code: api.CodeNotFound, Message: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, api.ErrorResponse{Code: api.CodeServerError, Message: "internal server error"})
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
	c.JSON(http.StatusOK, api.SuccessResponse{Code: api.CodeOK, Message: "trainer booking slots retrieved successfully", Data: &data, Meta: nil})
}

func (h *bookingHandler) HandleCreateBookingSession(c *gin.Context) {
	// Embed the generated body so existing fields bind unchanged, then
	// extend with messenger_handle which lives in api.yaml but isn't
	// in gen.go yet (codegen catch-up tracked separately).
	var request struct {
		api.CreateBookingJSONBody
		MessengerHandle *string `json:"messenger_handle,omitempty"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		h.log.Warn("error binding request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request", api.CodeBadRequest))
		return
	}
	// if trainer is not provided
	var fieldErrors []api.FieldError
	if request.TrainerId == uuid.Nil {
		h.log.Warn("HandleCreateBookingSession: trainer_id is required")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "trainers", Message: "please provide a trainer to be booked"})
	}
	if !common.IsNotEmpty(request.Timezone) {
		h.log.Warn("HandleCreateBookingSession: timezone is required")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "timezone", Message: "please provide current timezone"})
	}
	// Check if booking slot is available
	if request.ScheduledStart.IsZero() {
		h.log.Warn("HandleCreateBookingSession: scheduled_start is required")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "bookingSlot", Message: "select a booking start time"})
	}
	if request.ScheduledEnd.IsZero() {
		h.log.Warn("HandleCreateBookingSession: scheduled_end is required")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "bookingSlot", Message: "select a booking end time"})
	}
	if !request.ScheduledStart.IsZero() && !request.ScheduledEnd.IsZero() && request.ScheduledEnd.Before(request.ScheduledStart) {
		h.log.Warn("HandleCreateBookingSession: scheduled_end before scheduled_start")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "bookingSlot", Message: "booking end time must be after start time"})
	}
	if !request.ScheduledStart.IsZero() && request.ScheduledStart.Before(time.Now()) {
		h.log.Warn("HandleCreateBookingSession: scheduled_start is in the past")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "bookingSlot", Message: "booking start time must be in the future"})
	}
	// session_platform validation: accept the values the bookings
	// CHECK constraint allows after migration 000058. The generated
	// `request.SessionPlatform.Valid()` enum doesn't know about
	// `messenger` yet — same staleness reason as the embed above.
	platformStr := string(request.SessionPlatform)
	if !common.IsNotEmpty(platformStr) {
		h.log.Warn("HandleCreateBookingSession: session_platform is required")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "sessionPlatform", Message: "select a session platform"})
	} else {
		switch platformStr {
		case "zoom", "google_meet", "messenger":
			// ok
		default:
			h.log.Warn("HandleCreateBookingSession: invalid session_platform", "value", platformStr)
			fieldErrors = append(fieldErrors, api.FieldError{Field: "sessionPlatform", Message: "select a valid session platform: zoom, google_meet, or messenger"})
		}
	}
	// messenger_handle is required when platform=messenger; otherwise
	// optional + ignored. Free-form text up to 255 chars (cap matches
	// the discovery handler's validation; Facebook handles vary wildly
	// in format so we don't try to match a pattern).
	var messengerHandle string
	if request.MessengerHandle != nil {
		messengerHandle = strings.TrimSpace(*request.MessengerHandle)
	}
	if platformStr == "messenger" && messengerHandle == "" {
		fieldErrors = append(fieldErrors, api.FieldError{Field: "messenger_handle", Message: "messenger_handle is required when session_platform is messenger"})
	}
	if len(messengerHandle) > 255 {
		fieldErrors = append(fieldErrors, api.FieldError{Field: "messenger_handle", Message: "messenger_handle must not exceed 255 characters"})
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
	var messengerNS sql.NullString
	if messengerHandle != "" {
		messengerNS = sql.NullString{Valid: true, String: messengerHandle}
	}
	data := &db.CreateBookingParams{
		TrainerID:       request.TrainerId,
		ClientID:        userID,
		ScheduledStart:  sql.NullTime{Valid: true, Time: request.ScheduledStart},
		ScheduledEnd:    sql.NullTime{Valid: true, Time: request.ScheduledEnd},
		BookingStatus:   sql.NullString{Valid: true, String: defaultBookingStatus},
		SessionPlatform: sql.NullString{Valid: true, String: platformStr},
		MessengerHandle: messengerNS,
		Timezone:        sql.NullString{Valid: true, String: request.Timezone},
	}
	userData, err := h.service.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			h.log.Warn("HandleCreateBookingSession: user not found", "userID", userID)
			c.JSON(http.StatusNotFound, api.NewError("failed to get user by id", api.CodeNotFound))
			return
		}
		h.log.Warn("HandleCreateBookingSession: DB error fetching user", "userID", userID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to get user by id", api.CodeServerError))
		return
	}
	trainer, err := h.service.GetTrainerDetails(c.Request.Context(), request.TrainerId)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			h.log.Warn("HandleCreateBookingSession: trainer not found", "trainerID", request.TrainerId)
			c.JSON(http.StatusNotFound, api.NewError("failed to get trainer by id", api.CodeNotFound))
			return
		}
		h.log.Warn("HandleCreateBookingSession: DB error fetching trainer", "trainerID", request.TrainerId, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to get trainer by id", api.CodeServerError))
		return
	}
	created, err := h.service.CreateBooking(c.Request.Context(), *data, *userData, *trainer)
	if err != nil {
		h.log.Error("failed to create booking session", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to create booking session", api.CodeServerError))
		return
	}

	// Invalidate the trainer's slot cache — the next slot lookup
	// for this trainer must hit the DB so the just-booked window
	// is excluded. Best-effort: a Redis error here doesn't fail the
	// booking, but the user might briefly see the booked slot
	// available until the 15-minute TTL on the stale entry expires.
	//
	// Key must be keyed on the TRAINER PROFILE id (trainers.id, the
	// route param at /booking-slots/{trainerId}) — NOT trainer.ID
	// from GetTrainerUserDetails which is actually the trainer's
	// users.id. Using the wrong one silently misses the cache entry
	// and stale availability sticks around until the TTL.
	if h.redis != nil {
		cacheKey := "booking-slots:trainer:" + request.TrainerId.String()
		if err := h.redis.Delete(c.Request.Context(), cacheKey); err != nil {
			h.log.Warn("HandleCreateBookingSession: failed to invalidate slot cache", "trainerID", request.TrainerId, "err", err)
		}
	}

	// Notify trainer about new booking
	if h.notif != nil {
		if _, notifErr := h.notif.SendNotificationToUser(c.Request.Context(), trainer.ID,
			"New Booking",
			"You have a new session booking from "+userData.Name+".",
			"booking-"+created.ID.String(),
		); notifErr != nil {
			h.log.Warn("booking notification to trainer failed", "trainerID", trainer.TrainerID, "err", notifErr)
		}
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
	return api.SuccessResponse{Code: api.CodeOK, Message: "Booking session created successfully", Data: &responseInterface, Meta: nil}
}
