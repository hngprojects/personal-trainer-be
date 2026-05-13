package discovery

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
	"github.com/hngprojects/personal-trainer-be/pkg/meeting"
)

type Handler struct {
	repo    Repository
	meeting meeting.Provider
	mailer  email.Mailer
	log     *slog.Logger
}

func NewHandler(repo Repository, meetingProvider meeting.Provider, mailer email.Mailer, log *slog.Logger) *Handler {
	return &Handler{repo: repo, meeting: meetingProvider, mailer: mailer, log: log}
}

// POST /bookings/discovery — public
func (h *Handler) BookDiscoveryCall(c *gin.Context) {
	var req api.BookDiscoveryCallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
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

	selectedTime := req.SelectedDatetime

	if err := h.validateAgainstSlots(c, selectedTime, req.Timezone); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
		return
	}

	count, err := h.repo.CheckSlotConflict(c.Request.Context(), selectedTime)
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
	if req.ContactMode == api.ZoomMeeting {
		zoomLink, zoomMeetingID = h.createMeeting(c, selectedTime)
	}

	arg := db.CreateDiscoveryBookingParams{
		Name:             req.Name,
		Email:            string(req.Email),
		ContactMode:      string(req.ContactMode),
		PhoneNumber:      nullString(req.PhoneNumber),
		SelectedDatetime: selectedTime,
		ClientTimezone:   req.Timezone,
		ZoomMeetingLink:  sql.NullString{String: zoomLink, Valid: zoomLink != ""},
		ZoomMeetingID:    sql.NullString{String: zoomMeetingID, Valid: zoomMeetingID != ""},
	}

	booking, err := h.repo.CreateBooking(c.Request.Context(), arg)
	if err != nil {
		h.log.Error("failed to create discovery booking", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to create booking", api.CodeServerError))
		return
	}

	if err := h.mailer.SendDiscoveryBookingConfirmation(string(req.Email), req.Name, selectedTime, req.Timezone, zoomLink); err != nil {
		h.log.Error("failed to send booking confirmation email", "err", err, "email", req.Email)
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
		clientTZ = *params.Timezone
	}

	loc, err := time.LoadLocation(clientTZ)
	if err != nil {
		loc, _ = time.LoadLocation("Africa/Lagos")
	}

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

func (h *Handler) validateAgainstSlots(c *gin.Context, selectedTime time.Time, clientTZ string) error {
	slots, err := h.repo.GetActiveSlots(c.Request.Context())
	if err != nil || len(slots) == 0 {
		return fmt.Errorf("no available booking slots at this time")
	}

	loc, err := time.LoadLocation("Africa/Lagos")
	if err != nil {
		loc = time.UTC
	}

	watTime := selectedTime.In(loc)
	dayOfWeek := int16(watTime.Weekday())
	timeOfDay := watTime.Format("15:04")

	for _, s := range slots {
		slotStart := s.StartTime.Format("15:04")
		slotEnd := s.EndTime.Format("15:04")
		if s.DayOfWeek == dayOfWeek && timeOfDay >= slotStart && timeOfDay < slotEnd {
			return nil
		}
	}
	return fmt.Errorf("selected time is outside available booking hours")
}

func (h *Handler) createMeeting(c *gin.Context, startTime time.Time) (link, meetingID string) {
	if !h.meeting.IsConfigured() {
		h.log.Warn("meeting provider not configured — skipping meeting creation")
		return "", ""
	}
	link, meetingID, err := h.meeting.CreateMeeting("FitCall Discovery Call", startTime, 30)
	if err != nil {
		h.log.Error("meeting creation failed", "err", err)
		return "", ""
	}
	return link, meetingID
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
