package discovery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
	"github.com/hngprojects/personal-trainer-be/pkg/meeting"
)

type Handler struct {
	repo              Repository
	meeting           meeting.Provider
	mailer            email.Mailer
	notificationEmail string
	log               *slog.Logger
}

func NewHandler(repo Repository, meetingProvider meeting.Provider, mailer email.Mailer, notificationEmail string, log *slog.Logger) *Handler {
	return &Handler{repo: repo, meeting: meetingProvider, mailer: mailer, notificationEmail: notificationEmail, log: log}
}

// POST /bookings/discovery — authenticated users only
func (h *Handler) BookDiscoveryCall(c *gin.Context) {
	var req api.BookDiscoveryCallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
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

	if req.ContactMode != api.ZoomMeeting && req.ContactMode != api.PhoneCallback {
		c.JSON(http.StatusBadRequest, api.NewError("contact_mode must be zoom_meeting or phone_callback", api.CodeBadRequest))
		return
	}
	if req.ContactMode == api.PhoneCallback && (req.PhoneNumber == nil || *req.PhoneNumber == "") {
		c.JSON(http.StatusBadRequest, api.NewError("phone_number is required for phone_callback", api.CodeBadRequest))
		return
	}

	if _, err := time.LoadLocation(req.Timezone); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid timezone: must be a valid IANA timezone (e.g. America/New_York)", api.CodeBadRequest))
		return
	}

	selectedTime := req.SelectedDatetime
	ctx := c.Request.Context()

	// Reject past datetimes.
	if selectedTime.Before(time.Now()) {
		c.JSON(http.StatusBadRequest, api.NewError("selected time is in the past", api.CodeBadRequest))
		return
	}

	// One free discovery call per user.
	exists, err := h.repo.HasExistingBooking(ctx, userID)
	if err != nil {
		h.log.Error("failed to check existing booking", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	if exists {
		c.JSON(http.StatusForbidden, api.NewError("you have already used your free discovery call — please upgrade to book a session", api.CodeForbidden))
		return
	}

	if err := h.validateAgainstSlots(ctx, selectedTime); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
		return
	}

	count, err := h.repo.CheckSlotConflict(ctx, selectedTime)
	if err != nil {
		h.log.Error("failed to check slot conflict", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	if count > 0 {
		c.JSON(http.StatusConflict, api.NewError("this time slot is already taken", api.CodeConflict))
		return
	}

	var zoomLink, zoomMeetingID string

	phoneNumber := ""
	if req.PhoneNumber != nil {
		phoneNumber = *req.PhoneNumber
	}

	// Insert booking first; create Zoom meeting after so no orphaned meeting on DB failure.
	booking, err := h.repo.CreateBooking(ctx, db.CreateDiscoveryBookingParams{
		UserID:           uuid.NullUUID{UUID: userID, Valid: true},
		Name:             req.Name,
		Email:            string(req.Email),
		ContactMode:      string(req.ContactMode),
		PhoneNumber:      nullString(req.PhoneNumber),
		SelectedDatetime: selectedTime,
		ClientTimezone:   req.Timezone,
	})
	if err != nil {
		// Unique index violation means a concurrent request won the race for this slot.
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" && pqErr.Constraint == "idx_discovery_bookings_slot_lock" {
			c.JSON(http.StatusConflict, api.NewError("this time slot is already taken", api.CodeConflict))
			return
		}
		h.log.Error("failed to create discovery booking", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to create booking", api.CodeServerError))
		return
	}

	if req.ContactMode == api.ZoomMeeting {
		zoomLink, zoomMeetingID = h.createMeetingWithRetry(ctx, selectedTime)
		if zoomLink != "" {
			if updated, err := h.repo.UpdateBookingZoom(ctx, db.UpdateDiscoveryBookingZoomParams{
				ID:              booking.ID,
				ZoomMeetingLink: sql.NullString{String: zoomLink, Valid: true},
				ZoomMeetingID:   sql.NullString{String: zoomMeetingID, Valid: true},
			}); err == nil {
				booking = updated
			} else {
				h.log.Error("failed to persist zoom link", "err", err, "booking_id", booking.ID)
			}
		}
	}

	if err := h.mailer.SendDiscoveryBookingConfirmation(string(req.Email), req.Name, selectedTime, req.Timezone, string(req.ContactMode), phoneNumber, zoomLink); err != nil {
		h.log.Error("failed to send booking confirmation email", "err", err, "email", req.Email)
	}

	if h.notificationEmail != "" {
		if err := h.mailer.SendDiscoveryBookingAdminNotification(h.notificationEmail, req.Name, string(req.Email), selectedTime, req.Timezone, string(req.ContactMode), phoneNumber, zoomLink); err != nil {
			h.log.Error("failed to send admin notification email", "err", err)
		}
	}

	c.JSON(http.StatusCreated, api.NewSuccess("Discovery call booked successfully", api.CodeCreated, bookingToMap(booking)))
}

// GET /booking-slots — public
func (h *Handler) GetBookingSlots(c *gin.Context, params api.GetBookingSlotsParams) {
	slots, err := h.repo.GetActiveSlots(c.Request.Context())
	if err != nil {
		h.log.Error("failed to get booking slots", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	clientTZ := "Africa/Lagos"
	if params.Timezone != nil && *params.Timezone != "" {
		if _, err := time.LoadLocation(*params.Timezone); err == nil {
			clientTZ = *params.Timezone
		}
	}

	loc, _ := time.LoadLocation(clientTZ)

	list := make([]map[string]interface{}, 0, len(slots))
	for _, s := range slots {
		list = append(list, slotToMap(s, loc))
	}

	c.JSON(http.StatusOK, api.NewSuccess("Booking slots retrieved", api.CodeOK, list))
}

// POST /booking-slots — admin / customer_care
func (h *Handler) CreateBookingSlot(c *gin.Context) {
	var req api.BookingSlotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	tz := "Africa/Lagos"
	if req.Timezone != nil {
		if _, err := time.LoadLocation(*req.Timezone); err != nil {
			c.JSON(http.StatusBadRequest, api.NewError("invalid timezone", api.CodeBadRequest))
			return
		}
		tz = *req.Timezone
	}

	startT, err := time.Parse("15:04", req.StartTime)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("start_time must be HH:MM format", api.CodeBadRequest))
		return
	}
	endT, err := time.Parse("15:04", req.EndTime)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("end_time must be HH:MM format", api.CodeBadRequest))
		return
	}

	slot, err := h.repo.CreateSlot(c.Request.Context(), db.CreateBookingSlotParams{
		DayOfWeek: int16(req.DayOfWeek),
		StartTime: startT,
		EndTime:   endT,
		Timezone:  tz,
	})
	if err != nil {
		h.log.Error("failed to create booking slot", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to create slot", api.CodeServerError))
		return
	}

	c.JSON(http.StatusCreated, api.NewSuccess("Booking slot created", api.CodeCreated, slotToMap(slot, nil)))
}

// PUT /booking-slots/{id} — admin / customer_care
func (h *Handler) UpdateBookingSlot(c *gin.Context, id openapi_types.UUID) {
	var req api.BookingSlotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	slotID := uuid.UUID(id)
	if _, err := h.repo.GetSlotByID(c.Request.Context(), slotID); err != nil {
		c.JSON(http.StatusNotFound, api.NewNotFoundError("booking slot"))
		return
	}

	tz := "Africa/Lagos"
	if req.Timezone != nil {
		if _, err := time.LoadLocation(*req.Timezone); err != nil {
			c.JSON(http.StatusBadRequest, api.NewError("invalid timezone", api.CodeBadRequest))
			return
		}
		tz = *req.Timezone
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	startT, err := time.Parse("15:04", req.StartTime)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("start_time must be HH:MM format", api.CodeBadRequest))
		return
	}
	endT, err := time.Parse("15:04", req.EndTime)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("end_time must be HH:MM format", api.CodeBadRequest))
		return
	}

	updated, err := h.repo.UpdateSlot(c.Request.Context(), db.UpdateBookingSlotParams{
		ID:        slotID,
		DayOfWeek: int16(req.DayOfWeek),
		StartTime: startT,
		EndTime:   endT,
		Timezone:  tz,
		IsActive:  isActive,
	})
	if err != nil {
		h.log.Error("failed to update booking slot", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to update slot", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("Booking slot updated", api.CodeOK, slotToMap(updated, nil)))
}

// DELETE /booking-slots/{id} — admin / customer_care
func (h *Handler) DeleteBookingSlot(c *gin.Context, id openapi_types.UUID) {
	slotID := uuid.UUID(id)
	if _, err := h.repo.GetSlotByID(c.Request.Context(), slotID); err != nil {
		c.JSON(http.StatusNotFound, api.NewNotFoundError("booking slot"))
		return
	}

	if err := h.repo.DeleteSlot(c.Request.Context(), slotID); err != nil {
		h.log.Error("failed to delete booking slot", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to delete slot", api.CodeServerError))
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) validateAgainstSlots(ctx context.Context, selectedTime time.Time) error {
	slots, err := h.repo.GetActiveSlots(ctx)
	if err != nil {
		return fmt.Errorf("could not retrieve booking slots")
	}
	if len(slots) == 0 {
		return fmt.Errorf("no booking slots are currently configured")
	}
	for _, s := range slots {
		loc, err := time.LoadLocation(s.Timezone)
		if err != nil {
			loc = time.UTC
		}
		local := selectedTime.In(loc)
		dayOfWeek := int16(local.Weekday())
		timeOfDay := local.Format("15:04")
		slotStart := s.StartTime.Format("15:04")
		slotEnd := s.EndTime.Format("15:04")
		if s.DayOfWeek == dayOfWeek && timeOfDay >= slotStart && timeOfDay < slotEnd {
			return nil
		}
	}
	return fmt.Errorf("selected time is outside available booking hours")
}

func (h *Handler) createMeetingWithRetry(ctx context.Context, startTime time.Time) (link, meetingID string) {
	if !h.meeting.IsConfigured() {
		h.log.Warn("meeting provider not configured — skipping meeting creation")
		return "", ""
	}
	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		l, id, err := h.meeting.CreateMeeting(ctx, "FitCall Discovery Call", startTime, 30)
		if err == nil {
			return l, id
		}
		h.log.Warn("zoom meeting creation failed, retrying", "attempt", attempt, "err", err)
		if attempt < maxAttempts {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}
	h.log.Error("zoom meeting creation failed after all retries")
	return "", ""
}

func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func bookingToMap(b db.DiscoveryBooking) map[string]interface{} {
	m := map[string]interface{}{
		"id":                b.ID,
		"name":              b.Name,
		"email":             b.Email,
		"contact_mode":      b.ContactMode,
		"selected_datetime": b.SelectedDatetime,
		"client_timezone":   b.ClientTimezone,
		"status":            b.Status,
		"created_at":        b.CreatedAt,
	}
	if b.PhoneNumber.Valid {
		m["phone_number"] = b.PhoneNumber.String
	}
	if b.ZoomMeetingLink.Valid {
		m["zoom_meeting_link"] = b.ZoomMeetingLink.String
	}
	if b.ZoomMeetingID.Valid {
		m["zoom_meeting_id"] = b.ZoomMeetingID.String
	}
	return m
}

func slotToMap(s db.BookingSlot, loc *time.Location) map[string]interface{} {
	m := map[string]interface{}{
		"id":          s.ID,
		"day_of_week": s.DayOfWeek,
		"start_time":  s.StartTime.Format("15:04"),
		"end_time":    s.EndTime.Format("15:04"),
		"timezone":    s.Timezone,
		"is_active":   s.IsActive,
	}
	if loc != nil {
		m["display_timezone"] = loc.String()
	}
	return m
}
