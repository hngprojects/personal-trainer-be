package discovery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"sort"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/internal/notification"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
	"github.com/hngprojects/personal-trainer-be/pkg/meeting"
)

var phoneE164Regex = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)

type Handler struct {
	repo Repository
	// meetings is a Selector rather than a single Provider so trainers
	// with their own connected Zoom can host instead of the org account.
	// Discovery calls happen before a trainer is assigned, so this path
	// always asks the Selector with uuid.Nil — Selector implementations
	// fall back to the org provider for nil trainer IDs.
	meetings          meeting.Selector
	mailer            email.Mailer
	notificationEmail string
	log               *slog.Logger
	// notif is optional — nil means the route still works but no
	// admin / trainer notifications fire. Matches the pattern used in
	// internal/bookings/handler.go so the package doesn't hard-depend
	// on the notification service being wired.
	notif *notification.NotificationService
}

func NewHandler(repo Repository, meetingSelector meeting.Selector, mailer email.Mailer, notificationEmail string, log *slog.Logger, notif *notification.NotificationService) *Handler {
	return &Handler{repo: repo, meetings: meetingSelector, mailer: mailer, notificationEmail: notificationEmail, log: log, notif: notif}
}

// orgMeeting returns the Provider for un-attributed meetings (discovery
// calls before matching). Always uuid.Nil → Selector falls back to org.
// The platform argument routes between zoom and google_meet — the
// `phone_callback` and `messenger` contact_modes never reach this
// path (no server-minted URL for them).
func (h *Handler) orgMeeting(ctx context.Context, platform string) meeting.Provider {
	return h.meetings.For(ctx, uuid.Nil, platform)
}

// contactModeToPlatform maps the discovery_bookings.contact_mode
// values that DO have a meeting provider to the platform string the
// Selector expects. Returns "" for modes with no provider
// (phone_callback, messenger).
func contactModeToPlatform(mode string) string {
	switch mode {
	case "zoom_meeting":
		return meeting.PlatformZoom
	case "google_meet":
		return meeting.PlatformGoogleMeet
	default:
		return ""
	}
}

// POST /bookings/discovery — authenticated users only
func (h *Handler) BookDiscoveryCall(c *gin.Context) {
	// Embed the generated request type so all its existing fields
	// (contact_mode, phone_number, etc.) still bind verbatim, then
	// extend with messenger_handle which lives in api.yaml but isn't
	// in the on-disk gen.go yet (codegen catch-up is tracked
	// separately).
	var req struct {
		api.BookDiscoveryCallRequest
		MessengerHandle *string `json:"messenger_handle,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Warn("book discovery call: invalid request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	userIDVal, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		h.log.Warn("book discovery call: missing authenticated user")
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		h.log.Warn("book discovery call: invalid user id in context")
		c.JSON(http.StatusUnauthorized, api.NewError("invalid user id", api.CodeUnauthorized))
		return
	}

	// contact_mode values that the discovery_bookings CHECK accepts
	// after migration 000058. Kept as raw strings (not the api.*
	// generated constants) until oapi-codegen catches up to the new
	// enum entries in api.yaml — the gen.go regen is tracked
	// separately and forcing it now would surface unrelated
	// stale-stub issues. Spec still names these correctly; runtime
	// honours them.
	mode := string(req.ContactMode)
	switch mode {
	case "zoom_meeting", "phone_callback", "google_meet", "messenger", "imessage":
		// ok
	default:
		h.log.Warn("book discovery call: invalid contact mode", "userID", userID.String(), "contactMode", mode)
		c.JSON(http.StatusBadRequest, api.NewError("contact_mode must be zoom_meeting, google_meet, phone_callback, or messenger", api.CodeBadRequest))
		return
	}
	if mode == "phone_callback" && (req.PhoneNumber == nil || *req.PhoneNumber == "") {
		h.log.Warn("book discovery call: phone number required for callback", "userID", userID.String())
		c.JSON(http.StatusBadRequest, api.NewError("phone_number is required for phone_callback", api.CodeBadRequest))
		return
	}
	if mode == "imessage" && (req.PhoneNumber == nil || *req.PhoneNumber == "") {
		h.log.Warn("book discovery call: phone number required for imessage", "userID", userID.String())
		c.JSON(http.StatusBadRequest, api.NewError("phone_number is required for iMessage", api.CodeBadRequest))
		return
	}
	// Messenger is the same shape as phone_callback — client supplies
	// a free-form handle, no server-minted URL. Validation is
	// permissive: handles can be m.me URLs, profile slugs, or numeric
	// IDs depending on which the user copies in. Cap length so a
	// malformed paste doesn't bloat the column.
	var messengerHandle string
	if req.MessengerHandle != nil {
		messengerHandle = strings.TrimSpace(*req.MessengerHandle)
	}
	if mode == "messenger" && messengerHandle == "" {
		h.log.Warn("book discovery call: messenger handle required for messenger contact mode", "userID", userID.String())
		c.JSON(http.StatusBadRequest, api.NewError("messenger_handle is required for messenger contact mode", api.CodeBadRequest))
		return
	}
	if messengerHandle != "" && len(messengerHandle) > 255 {
		c.JSON(http.StatusBadRequest, api.NewError("messenger_handle must not exceed 255 characters", api.CodeBadRequest))
		return
	}
	if req.PhoneNumber != nil && *req.PhoneNumber != "" {
		if !phoneE164Regex.MatchString(*req.PhoneNumber) {
			h.log.Warn("book discovery call: invalid phone number format", "userID", userID.String())
			c.JSON(http.StatusBadRequest, api.NewError("phone_number must be in E.164 format (e.g. +2348012345678)", api.CodeBadRequest))
			return
		}
	}

	if _, err := time.LoadLocation(req.Timezone); err != nil {
		h.log.Warn("book discovery call: invalid timezone", "userID", userID.String(), "timezone", req.Timezone, "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid timezone: must be a valid IANA timezone (e.g. America/New_York)", api.CodeBadRequest))
		return
	}

	selectedTime := req.SelectedDatetime
	ctx := c.Request.Context()

	// Reject past datetimes.
	if selectedTime.Before(time.Now()) {
		h.log.Warn("book discovery call: selected time is in the past", "userID", userID.String(), "selectedTime", selectedTime)
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
		h.log.Warn("book discovery call: user already used free discovery call", "userID", userID.String())
		c.JSON(http.StatusForbidden, api.NewError("you have already used your free discovery call — please upgrade to book a session", api.CodeForbidden))
		return
	}

	if err := h.validateAgainstSlots(ctx, selectedTime); err != nil {
		h.log.Warn("book discovery call: slot validation failed", "userID", userID.String(), "selectedTime", selectedTime, "err", err)
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
		h.log.Warn("book discovery call: slot already taken", "userID", userID.String(), "selectedTime", selectedTime)
		c.JSON(http.StatusConflict, api.NewError("this time slot is already taken", api.CodeConflict))
		return
	}

	var zoomLink, zoomMeetingID string

	phoneNumber := ""
	if req.PhoneNumber != nil {
		phoneNumber = *req.PhoneNumber
	}

	// Insert booking first; create Zoom meeting after so no orphaned meeting on DB failure.
	var messengerNS sql.NullString
	if messengerHandle != "" {
		messengerNS = sql.NullString{String: messengerHandle, Valid: true}
	}
	booking, err := h.repo.CreateBooking(ctx, db.CreateDiscoveryBookingParams{
		UserID:           uuid.NullUUID{UUID: userID, Valid: true},
		Name:             req.Name,
		Email:            string(req.Email),
		ContactMode:      mode,
		PhoneNumber:      nullString(req.PhoneNumber),
		MessengerHandle:  messengerNS,
		SelectedDatetime: selectedTime,
		ClientTimezone:   req.Timezone,
	})
	if err != nil {
		// Unique index violation means a concurrent request won the race for this slot.
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" && pqErr.Constraint == "idx_discovery_bookings_slot_lock" {
			h.log.Warn("book discovery call: slot already taken (unique constraint)", "userID", userID.String(), "selectedTime", selectedTime)
			c.JSON(http.StatusConflict, api.NewError("this time slot is already taken", api.CodeConflict))
			return
		}
		h.log.Error("failed to create discovery booking", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to create booking", api.CodeServerError))
		return
	}

	// Mint a meeting URL only for contact modes that actually have a
	// server-side provider. `phone_callback` and `messenger` are
	// handle-only modes — nothing to create.
	if platform := contactModeToPlatform(mode); platform != "" {
		zoomLink, zoomMeetingID = h.createMeetingWithRetry(ctx, platform, selectedTime)
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
		h.log.Error("failed to send booking confirmation email", "err", err, "email", req.Email, "booking_id", booking.ID)
	}

	if h.notificationEmail != "" {
		if err := h.mailer.SendDiscoveryBookingAdminNotification(h.notificationEmail, req.Name, string(req.Email), selectedTime, req.Timezone, string(req.ContactMode), phoneNumber, zoomLink); err != nil {
			h.log.Error("failed to send admin notification email", "err", err, "email", h.notificationEmail, "booking_id", booking.ID)
		}
	}

	// In-app notification to every admin so the staff dashboard shows
	// new discovery requests without anyone having to refresh. The
	// table-wide UNIQUE constraint on idempotency_key is per-admin
	// inside the broadcast helper.
	if h.notif != nil {
		if _, notifErr := h.notif.SendNotificationToAdmins(ctx,
			"New Discovery Call",
			req.Name+" booked a discovery call.",
			"discovery-booked-"+booking.ID.String(),
		); notifErr != nil {
			h.log.Warn("admin notification (discovery booked) failed", "booking_id", booking.ID, "err", notifErr)
		}
	}

	c.JSON(http.StatusCreated, api.NewSuccess("Discovery call booked successfully", api.CodeCreated, bookingToMap(booking)))
}

// GET /booking-slots — public
func (h *Handler) GetDiscoverySlots(c *gin.Context, params api.GetDiscoverySlotsParams) {
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
func (h *Handler) CreateDiscoverySlot(c *gin.Context) {
	var req api.BookingSlotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Warn("create discovery slot: invalid request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	tz := "Africa/Lagos"
	if req.Timezone != nil {
		if _, err := time.LoadLocation(*req.Timezone); err != nil {
			h.log.Warn("create discovery slot: invalid timezone", "timezone", *req.Timezone, "err", err)
			c.JSON(http.StatusBadRequest, api.NewError("invalid timezone: must be a valid IANA timezone (e.g. America/New_York)", api.CodeBadRequest))
			return
		}
		tz = *req.Timezone
	}

	if req.DayOfWeek < 0 || req.DayOfWeek > 6 {
		h.log.Warn("create discovery slot: invalid day of week", "dayOfWeek", req.DayOfWeek)
		c.JSON(http.StatusBadRequest, api.NewError("day_of_week must be 0 (Sunday) to 6 (Saturday)", api.CodeBadRequest))
		return
	}

	startT, err := time.Parse("15:04", req.StartTime)
	if err != nil {
		h.log.Warn("create discovery slot: invalid start time", "startTime", req.StartTime, "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("start_time must be HH:MM format", api.CodeBadRequest))
		return
	}
	endT, err := time.Parse("15:04", req.EndTime)
	if err != nil {
		h.log.Warn("create discovery slot: invalid end time", "endTime", req.EndTime, "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("end_time must be HH:MM format", api.CodeBadRequest))
		return
	}
	if !startT.Before(endT) {
		h.log.Warn("create discovery slot: end time before start time", "startTime", req.StartTime, "endTime", req.EndTime)
		c.JSON(http.StatusBadRequest, api.NewError("start_time must be before end_time", api.CodeBadRequest))
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

// CreateDiscoverySlotsBulk handles POST /discovery-slots/bulk — additive
// batch insert of multiple slots in a single transaction. The whole batch
// rolls back on any per-row failure (parse error, overlap, duplicate of
// an existing slot), so partial inserts can't happen.
func (h *Handler) CreateDiscoverySlotsBulk(c *gin.Context) {
	var req api.BookingSlotsBulkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Warn("create discovery slots bulk: invalid request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}
	if len(req.Slots) == 0 {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "slots", Message: "at least one slot is required"},
		}))
		return
	}
	if len(req.Slots) > 100 {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "slots", Message: "at most 100 slots per request"},
		}))
		return
	}

	// Parse + per-slot validation. We collect everything before the DB
	// hit so a bad slot at index 4 doesn't trigger an INSERT for slot 0–3
	// that has to roll back.
	type parsed struct {
		dayOfWeek int16
		start     time.Time
		end       time.Time
		timezone  string
	}
	parsedSlots := make([]parsed, 0, len(req.Slots))
	for i, s := range req.Slots {
		tz := "Africa/Lagos"
		if s.Timezone != nil {
			if _, err := time.LoadLocation(*s.Timezone); err != nil {
				c.JSON(http.StatusBadRequest, api.NewError(fmt.Sprintf("slot %d: invalid timezone", i), api.CodeBadRequest))
				return
			}
			tz = *s.Timezone
		}
		if s.DayOfWeek < 0 || s.DayOfWeek > 6 {
			c.JSON(http.StatusBadRequest, api.NewError(fmt.Sprintf("slot %d: day_of_week must be 0 (Sunday) to 6 (Saturday)", i), api.CodeBadRequest))
			return
		}
		startT, err := time.Parse("15:04", s.StartTime)
		if err != nil {
			c.JSON(http.StatusBadRequest, api.NewError(fmt.Sprintf("slot %d: start_time must be HH:MM format", i), api.CodeBadRequest))
			return
		}
		endT, err := time.Parse("15:04", s.EndTime)
		if err != nil {
			c.JSON(http.StatusBadRequest, api.NewError(fmt.Sprintf("slot %d: end_time must be HH:MM format", i), api.CodeBadRequest))
			return
		}
		if !startT.Before(endT) {
			c.JSON(http.StatusBadRequest, api.NewError(fmt.Sprintf("slot %d: start_time must be before end_time", i), api.CodeBadRequest))
			return
		}
		parsedSlots = append(parsedSlots, parsed{
			dayOfWeek: int16(s.DayOfWeek),
			start:     startT,
			end:       endT,
			timezone:  tz,
		})
	}

	// In-request overlap check: pairwise compare on same day_of_week.
	// O(n^2) is fine — the request is capped at 100.
	for i := 0; i < len(parsedSlots); i++ {
		for j := i + 1; j < len(parsedSlots); j++ {
			a, b := parsedSlots[i], parsedSlots[j]
			if a.dayOfWeek != b.dayOfWeek {
				continue
			}
			if a.start.Before(b.end) && a.end.After(b.start) {
				c.JSON(http.StatusBadRequest, api.NewError(fmt.Sprintf("slots %d and %d overlap on same day_of_week", i, j), api.CodeBadRequest))
				return
			}
		}
	}

	// Convert to DB params and run inside one TX.
	params := make([]db.CreateBookingSlotParams, len(parsedSlots))
	for i, p := range parsedSlots {
		params[i] = db.CreateBookingSlotParams{
			DayOfWeek: p.dayOfWeek,
			StartTime: p.start,
			EndTime:   p.end,
			Timezone:  p.timezone,
		}
	}

	saved, err := h.repo.CreateSlotsBulk(c.Request.Context(), params)
	if err != nil {
		h.log.Error("failed to create booking slots bulk", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to create slots", api.CodeServerError))
		return
	}

	resp := make([]map[string]interface{}, 0, len(saved))
	for _, s := range saved {
		resp = append(resp, slotToMap(s, nil))
	}
	c.JSON(http.StatusCreated, api.NewSuccess("Booking slots created", api.CodeCreated, resp))
}

// PUT /booking-slots/{id} — admin / customer_care
func (h *Handler) UpdateDiscoverySlot(c *gin.Context, id openapi_types.UUID) {
	var req api.BookingSlotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Warn("update discovery slot: invalid request body", "slotID", uuid.UUID(id).String(), "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	slotID := uuid.UUID(id)
	if _, err := h.repo.GetSlotByID(c.Request.Context(), slotID); err != nil {
		h.log.Warn("update discovery slot: slot not found", "slotID", slotID.String(), "err", err)
		c.JSON(http.StatusNotFound, api.NewNotFoundError("booking slot"))
		return
	}

	tz := "Africa/Lagos"
	if req.Timezone != nil {
		if _, err := time.LoadLocation(*req.Timezone); err != nil {
			h.log.Warn("update discovery slot: invalid timezone", "slotID", slotID.String(), "timezone", *req.Timezone, "err", err)
			c.JSON(http.StatusBadRequest, api.NewError("invalid timezone: must be a valid IANA timezone (e.g. America/New_York)", api.CodeBadRequest))
			return
		}
		tz = *req.Timezone
	}

	if req.DayOfWeek < 0 || req.DayOfWeek > 6 {
		h.log.Warn("update discovery slot: invalid day of week", "slotID", slotID.String(), "dayOfWeek", req.DayOfWeek)
		c.JSON(http.StatusBadRequest, api.NewError("day_of_week must be 0 (Sunday) to 6 (Saturday)", api.CodeBadRequest))
		return
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	startT, err := time.Parse("15:04", req.StartTime)
	if err != nil {
		h.log.Warn("update discovery slot: invalid start time", "slotID", slotID.String(), "startTime", req.StartTime, "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("start_time must be HH:MM format", api.CodeBadRequest))
		return
	}
	endT, err := time.Parse("15:04", req.EndTime)
	if err != nil {
		h.log.Warn("update discovery slot: invalid end time", "slotID", slotID.String(), "endTime", req.EndTime, "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("end_time must be HH:MM format", api.CodeBadRequest))
		return
	}
	if !startT.Before(endT) {
		h.log.Warn("update discovery slot: end time before start time", "slotID", slotID.String(), "startTime", req.StartTime, "endTime", req.EndTime)
		c.JSON(http.StatusBadRequest, api.NewError("start_time must be before end_time", api.CodeBadRequest))
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
func (h *Handler) DeleteDiscoverySlot(c *gin.Context, id openapi_types.UUID) {
	slotID := uuid.UUID(id)
	if _, err := h.repo.GetSlotByID(c.Request.Context(), slotID); err != nil {
		h.log.Warn("delete discovery slot: slot not found", "slotID", slotID.String(), "err", err)
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

// PUT /bookings/:id/reschedule — authenticated users only
func (h *Handler) RescheduleDiscoveryCall(c *gin.Context, id openapi_types.UUID) {
	bookingID := uuid.UUID(id)
	ctx := c.Request.Context()

	userIDVal, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		h.log.Warn("reschedule discovery call: missing authenticated user", "bookingID", bookingID.String())
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		h.log.Warn("reschedule discovery call: invalid user id in context", "bookingID", bookingID.String())
		c.JSON(http.StatusUnauthorized, api.NewError("invalid user id", api.CodeUnauthorized))
		return
	}

	var req api.RescheduleBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Warn("reschedule discovery call: invalid request body", "bookingID", bookingID.String(), "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	if !req.Reason.Valid() {
		h.log.Warn("reschedule discovery call: invalid reason", "bookingID", bookingID.String(), "userID", userID.String())
		c.JSON(http.StatusBadRequest, api.NewError("invalid reason", api.CodeBadRequest))
		return
	}

	if _, err := time.LoadLocation(req.Timezone); err != nil {
		h.log.Warn("reschedule discovery call: invalid timezone", "bookingID", bookingID.String(), "userID", userID.String(), "timezone", req.Timezone, "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid timezone: must be a valid IANA timezone (e.g. America/New_York)", api.CodeBadRequest))
		return
	}

	if req.Notes != nil && len(*req.Notes) > 200 {
		h.log.Warn("reschedule discovery call: notes too long", "bookingID", bookingID.String(), "userID", userID.String(), "notesLength", len(*req.Notes))
		c.JSON(http.StatusBadRequest, api.NewError("notes must not exceed 200 characters", api.CodeBadRequest))
		return
	}

	booking, err := h.repo.GetBookingByID(ctx, bookingID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.log.Warn("reschedule discovery call: booking not found", "bookingID", bookingID.String(), "err", err)
			c.JSON(http.StatusNotFound, api.NewNotFoundError("booking"))
			return
		}
		h.log.Error("failed to get booking", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	// Ownership check
	if !booking.UserID.Valid || booking.UserID.UUID != userID {
		h.log.Warn("reschedule discovery call: ownership mismatch", "bookingID", bookingID.String(), "userID", userID.String(), "bookingUserID", booking.UserID.UUID)
		c.JSON(http.StatusForbidden, api.NewError("you do not have permission to reschedule this booking", api.CodeForbidden))
		return
	}

	// Status checks
	if booking.Status == "cancelled" {
		h.log.Warn("reschedule discovery call: booking is cancelled", "bookingID", bookingID.String(), "userID", userID.String())
		c.JSON(http.StatusForbidden, api.NewError("cannot reschedule a cancelled booking", api.CodeForbidden))
		return
	}
	if booking.Status == "completed" {
		h.log.Warn("reschedule discovery call: booking is completed", "bookingID", bookingID.String(), "userID", userID.String())
		c.JSON(http.StatusForbidden, api.NewError("cannot reschedule a completed booking", api.CodeForbidden))
		return
	}

	// 12-hour lock window — all comparisons in UTC
	lockDeadline := booking.SelectedDatetime.UTC().Add(-12 * time.Hour)
	if !time.Now().UTC().Before(lockDeadline) {
		h.log.Warn("reschedule discovery call: within 12-hour lock window", "bookingID", bookingID.String(), "userID", userID.String(), "selectedDatetime", booking.SelectedDatetime)
		c.JSON(http.StatusForbidden, api.NewError("rescheduling is not allowed within 12 hours of the original call time", api.CodeForbidden))
		return
	}

	// Max 3 reschedules per booking
	if booking.RescheduleCount >= 3 {
		h.log.Warn("reschedule discovery call: max reschedule limit reached", "bookingID", bookingID.String(), "userID", userID.String(), "rescheduleCount", booking.RescheduleCount)
		c.JSON(http.StatusTooManyRequests, api.NewError("maximum reschedule limit of 3 has been reached for this booking", api.CodeTooManyRequests))
		return
	}

	newTime := req.NewDatetime

	// New time must not be in the past
	if newTime.Before(time.Now()) {
		h.log.Warn("reschedule discovery call: new time is in the past", "bookingID", bookingID.String(), "userID", userID.String(), "newTime", newTime)
		c.JSON(http.StatusBadRequest, api.NewError("new time is in the past", api.CodeBadRequest))
		return
	}

	// New time must fall within open hours
	if err := h.validateAgainstSlots(ctx, newTime); err != nil {
		h.log.Warn("reschedule discovery call: slot validation failed", "bookingID", bookingID.String(), "userID", userID.String(), "newTime", newTime, "err", err)
		c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
		return
	}

	// Slot conflict check (excluding this booking's current slot)
	count, err := h.repo.CheckSlotConflictExcluding(ctx, db.CheckSlotConflictExcludingParams{
		SelectedDatetime: newTime,
		ExcludeID:        bookingID,
	})
	if err != nil {
		h.log.Error("failed to check slot conflict", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	if count > 0 {
		h.log.Warn("reschedule discovery call: slot already taken", "bookingID", bookingID.String(), "userID", userID.String(), "newTime", newTime)
		c.JSON(http.StatusConflict, api.NewError("this time slot is already taken", api.CodeConflict))
		return
	}

	// Handle Zoom: create new meeting FIRST, update DB, then delete old.
	// This order prevents orphaned state — if the DB update fails we can
	// clean up the newly created meeting before returning an error.
	// Default to existing values so a Zoom creation failure does not wipe the old link.
	newZoomLink := booking.ZoomMeetingLink
	newZoomMeetingID := booking.ZoomMeetingID
	oldZoomMeetingID := ""
	// Reschedule keeps the same platform the original booking chose.
	// Only zoom_meeting and google_meet flow through here; phone +
	// messenger modes have no URL to refresh.
	reschedulePlatform := contactModeToPlatform(booking.ContactMode)
	if reschedulePlatform != "" {
		link, meetID := h.createMeetingWithRetry(ctx, reschedulePlatform, newTime)
		if link != "" {
			if booking.ZoomMeetingID.Valid {
				oldZoomMeetingID = booking.ZoomMeetingID.String
			}
			newZoomLink = sql.NullString{String: link, Valid: true}
			newZoomMeetingID = sql.NullString{String: meetID, Valid: true}
		}
	}

	// Resolve phone number: use updated value if provided, else keep existing
	phoneNumber := booking.PhoneNumber
	if booking.ContactMode == "phone_callback" && req.PhoneNumber != nil && *req.PhoneNumber != "" {
		if !phoneE164Regex.MatchString(*req.PhoneNumber) {
			h.log.Warn("reschedule discovery call: invalid phone number format", "bookingID", bookingID.String(), "userID", userID.String())
			c.JSON(http.StatusBadRequest, api.NewError("phone_number must be in E.164 format (e.g. +2348012345678)", api.CodeBadRequest))
			return
		}
		phoneNumber = sql.NullString{String: *req.PhoneNumber, Valid: true}
	}

	oldTime := booking.SelectedDatetime

	updated, err := h.repo.RescheduleBooking(ctx, db.RescheduleDiscoveryBookingParams{
		ID:               bookingID,
		SelectedDatetime: newTime,
		PhoneNumber:      phoneNumber,
		ZoomMeetingLink:  newZoomLink,
		ZoomMeetingID:    newZoomMeetingID,
	})
	if err != nil {
		// Clean up the newly created Zoom meeting to avoid orphaned state.
		// Use a background context — the request context may already be cancelled.
		if newZoomMeetingID.Valid {
			cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cleanCancel()
			if delErr := h.orgMeeting(cleanCtx, reschedulePlatform).DeleteMeeting(cleanCtx, newZoomMeetingID.String); delErr != nil {
				h.log.Error("orphaned zoom meeting after DB failure — manual cleanup required",
					"meeting_id", newZoomMeetingID.String, "err", delErr)
			}
		}
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" && pqErr.Constraint == "idx_discovery_bookings_slot_lock" {
			h.log.Warn("reschedule discovery call: slot already taken (unique constraint)", "bookingID", bookingID.String(), "userID", userID.String(), "newTime", newTime)
			c.JSON(http.StatusConflict, api.NewError("this time slot is already taken", api.CodeConflict))
			return
		}
		h.log.Error("failed to reschedule booking", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to reschedule booking", api.CodeServerError))
		return
	}

	// Delete old Zoom meeting only after DB is committed successfully.
	// Use a detached context so a disconnected client can't abort the cleanup.
	if oldZoomMeetingID != "" {
		delCtx, delCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer delCancel()
		if err := h.orgMeeting(delCtx, reschedulePlatform).DeleteMeeting(delCtx, oldZoomMeetingID); err != nil {
			h.log.Warn("failed to delete old zoom meeting — may require manual cleanup",
				"meeting_id", oldZoomMeetingID, "err", err)
		}
	}

	// Record history (non-fatal)
	var notes sql.NullString
	if req.Notes != nil {
		notes = sql.NullString{String: *req.Notes, Valid: true}
	}
	if err := h.repo.CreateRescheduleHistory(ctx, db.CreateRescheduleHistoryParams{
		DiscoveryBookingID: bookingID,
		PreviousDatetime:   oldTime,
		NewDatetime:        newTime,
		RescheduledBy:      "client",
		Reason:             sql.NullString{String: string(req.Reason), Valid: true},
		Notes:              notes,
	}); err != nil {
		h.log.Error("failed to record reschedule history", "err", err)
	}

	// Send confirmation email (non-fatal)
	finalZoomLink := ""
	if updated.ZoomMeetingLink.Valid {
		finalZoomLink = updated.ZoomMeetingLink.String
	}
	finalPhone := ""
	if updated.PhoneNumber.Valid {
		finalPhone = updated.PhoneNumber.String
	}
	if err := h.mailer.SendDiscoveryRescheduleConfirmation(
		updated.Email, updated.Name, oldTime, newTime,
		req.Timezone, updated.ContactMode, finalPhone, finalZoomLink,
	); err != nil {
		h.log.Error("failed to send reschedule confirmation email", "err", err, "email", updated.Email, "booking_id", updated.ID)
	}

	// Admin in-app notification mirrors the booking event so staff can
	// see schedule changes in their dashboard.
	//
	// Key includes RescheduleCount because the same booking can be
	// rescheduled up to maxReschedules times (currently 3). Without
	// the count the second/third reschedule would collide on the
	// UNIQUE(idempotency_key) constraint and be silently dropped as
	// "already delivered." reschedule_count is incremented BEFORE the
	// RETURNING in the UPDATE, so the value here is the 1-based
	// reschedule sequence number — distinct on every successful call.
	if h.notif != nil {
		key := fmt.Sprintf("discovery-rescheduled-%s-%d", updated.ID, updated.RescheduleCount)
		if _, notifErr := h.notif.SendNotificationToAdmins(ctx,
			"Discovery Call Rescheduled",
			updated.Name+" rescheduled their discovery call.",
			key,
		); notifErr != nil {
			h.log.Warn("admin notification (discovery rescheduled) failed", "booking_id", updated.ID, "err", notifErr)
		}
	}

	c.JSON(http.StatusOK, api.NewSuccess("Discovery call rescheduled successfully", api.CodeOK, bookingToMap(updated)))
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
			h.log.Warn("invalid timezone in booking slot, falling back to UTC", "slot_id", s.ID, "timezone", s.Timezone)
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

func (h *Handler) createMeetingWithRetry(ctx context.Context, platform string, startTime time.Time) (link, meetingID string) {
	prov := h.orgMeeting(ctx, platform)
	if !prov.IsConfigured() {
		h.log.Warn("meeting provider not configured — skipping meeting creation", "platform", platform)
		return "", ""
	}
	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return "", ""
		default:
		}
		l, id, err := prov.CreateMeeting(ctx, "FitCall Discovery Call", startTime, 30)
		if err == nil {
			return l, id
		}
		h.log.Warn("meeting creation failed, retrying", "platform", platform, "attempt", attempt, "err", err)
		if attempt < maxAttempts {
			select {
			case <-time.After(time.Duration(attempt) * time.Second):
			case <-ctx.Done():
				return "", ""
			}
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
	m["reschedule_count"] = b.RescheduleCount
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
	} else {
		m["display_timezone"] = s.Timezone
	}
	return m
}

// GET /bookings/upcoming — authenticated client
const discoveryCallDurationMinutes = 30

func (h *Handler) GetUpcomingBookings(c *gin.Context, params api.GetUpcomingBookingsParams) {
	ctx := c.Request.Context()

	userIDVal, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		h.log.Warn("get upcoming bookings: missing authenticated user")
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		h.log.Warn("get upcoming bookings: invalid user id in context")
		c.JSON(http.StatusUnauthorized, api.NewError("invalid user id", api.CodeUnauthorized))
		return
	}

	// Resolve timezone
	clientTZ := "UTC"
	if params.Timezone != nil && *params.Timezone != "" {
		if _, err := time.LoadLocation(*params.Timezone); err != nil {
			h.log.Warn("get upcoming bookings: invalid timezone", "userID", userID.String(), "timezone", *params.Timezone, "err", err)
			c.JSON(http.StatusBadRequest, api.NewError("invalid timezone: must be a valid IANA timezone (e.g. America/New_York)", api.CodeBadRequest))
			return
		}
		clientTZ = *params.Timezone
	}
	loc, _ := time.LoadLocation(clientTZ)

	// Validate and apply pagination params
	page := 1
	if params.Page != nil {
		if *params.Page < 1 {
			h.log.Warn("get upcoming bookings: invalid page", "userID", userID.String(), "page", *params.Page)
			c.JSON(http.StatusBadRequest, api.NewError("page must be >= 1", api.CodeBadRequest))
			return
		}
		page = *params.Page
	}
	limit := 10
	if params.Limit != nil {
		if *params.Limit < 1 || *params.Limit > 100 {
			h.log.Warn("get upcoming bookings: invalid limit", "userID", userID.String(), "limit", *params.Limit)
			c.JSON(http.StatusBadRequest, api.NewError("limit must be between 1 and 100", api.CodeBadRequest))
			return
		}
		limit = *params.Limit
	}

	type upcomingItem struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		// SessionID is the booking_session.id for paid sessions whose session
		// row has been created (i.e. the session was started). It lets the
		// client navigate from this list to GET /sessions/{id} without an
		// extra round trip. Omitted for discovery calls (they have no
		// associated session row) and for paid bookings that haven't been
		// started yet (the booking_session row only exists post-start).
		SessionID *string `json:"session_id,omitempty"`
		// TrainerID is the bookings.trainer_id for paid sessions — the client
		// uses it to look up trainer details (e.g. GET /trainers/{id}).
		// Omitted for discovery calls (no trainer assigned).
		TrainerID        *string   `json:"trainer_id,omitempty"`
		ScheduledAt      string    `json:"scheduled_at"`
		ScheduledAtLocal string    `json:"scheduled_at_local"`
		DurationMinutes  int       `json:"duration_minutes"`
		Status           string    `json:"status"`
		ContactMode      *string   `json:"contact_mode,omitempty"`
		ZoomMeetingLink  *string   `json:"zoom_meeting_link,omitempty"`
		PhoneNumber      *string   `json:"phone_number,omitempty"`
		TrainerName      *string   `json:"trainer_name,omitempty"`
		TrainerPhoto     *string   `json:"trainer_photo,omitempty"`
		Specializations  []string  `json:"specializations,omitempty"`
		SortKey          time.Time `json:"-"`
	}

	var items []upcomingItem

	filterType := ""
	if params.Type != nil {
		filterType = string(*params.Type)
	}

	// Fetch discovery calls
	if filterType == "" || filterType == string(api.DiscoveryCall) {
		discovery, err := h.repo.GetUpcomingDiscoveryBookings(ctx, userID)
		if err != nil {
			h.log.Error("failed to get upcoming discovery bookings", "err", err, "user_id", userID)
			c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
			return
		}
		for _, b := range discovery {
			mode := b.ContactMode
			item := upcomingItem{
				ID:               b.ID.String(),
				Type:             "discovery_call",
				ScheduledAt:      b.SelectedDatetime.UTC().Format(time.RFC3339),
				ScheduledAtLocal: b.SelectedDatetime.In(loc).Format(time.RFC3339),
				DurationMinutes:  discoveryCallDurationMinutes,
				Status:           b.Status,
				ContactMode:      &mode,
				SortKey:          b.SelectedDatetime,
			}
			if b.ZoomMeetingLink.Valid {
				v := b.ZoomMeetingLink.String
				item.ZoomMeetingLink = &v
			}
			if b.PhoneNumber.Valid {
				v := b.PhoneNumber.String
				item.PhoneNumber = &v
			}
			items = append(items, item)
		}
	}

	// Fetch paid sessions
	if filterType == "" || filterType == string(api.PaidSession) {
		sessions, err := h.repo.GetUpcomingPaidSessions(ctx, userID)
		if err != nil {
			h.log.Error("failed to get upcoming paid sessions", "err", err, "user_id", userID)
			c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
			return
		}
		for _, s := range sessions {
			if !s.ScheduledStart.Valid {
				continue
			}
			scheduledStart := s.ScheduledStart.Time
			durationMins := 60
			if s.ScheduledEnd.Valid {
				durationMins = int(s.ScheduledEnd.Time.Sub(scheduledStart).Minutes())
			}
			status := "confirmed"
			if s.BookingStatus.Valid {
				status = s.BookingStatus.String
			}
			trainerIDStr := s.TrainerID.String()
			item := upcomingItem{
				ID:               s.ID.String(),
				Type:             "paid_session",
				TrainerID:        &trainerIDStr,
				ScheduledAt:      scheduledStart.UTC().Format(time.RFC3339),
				ScheduledAtLocal: scheduledStart.In(loc).Format(time.RFC3339),
				DurationMinutes:  durationMins,
				Status:           status,
				SortKey:          scheduledStart,
			}
			if s.SessionPlatform.Valid {
				v := s.SessionPlatform.String
				item.ContactMode = &v
			}
			if s.TrainerName != "" {
				v := s.TrainerName
				item.TrainerName = &v
			}
			if len(s.TrainerSpecializations) > 0 {
				// Trainer specializations are now multi-valued (see migration
				// 033). Copy the slice so the response retains the full set
				// rather than the previous single-string field.
				item.Specializations = append([]string(nil), s.TrainerSpecializations...)
			}
			if s.TrainerPhoto.Valid {
				v := s.TrainerPhoto.String
				item.TrainerPhoto = &v
			}
			items = append(items, item)
		}
	}

	// Sort merged results by datetime ascending
	sort.Slice(items, func(i, j int) bool {
		return items[i].SortKey.Before(items[j].SortKey)
	})

	// Paginate
	total := len(items)
	totalPages := (total + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}
	offset := (page - 1) * limit
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	paged := items[offset:end]

	// Enrich paid-session items in the paged slice with their
	// booking_session.id (so the client can navigate straight to
	// /sessions/{id} from this list). Run AFTER pagination so the per-row
	// session lookup is bounded by `limit` (≤100), not by the total upcoming
	// count — a user with 200 upcoming sessions on a page-size-10 request
	// pays for 10 lookups, not 200.
	//
	// Discovery calls don't have an associated booking_session row, so we
	// skip them entirely. A per-row failure logs a warning and leaves
	// SessionID nil for that item — the client can retry the detail call.
	for i := range paged {
		if paged[i].Type != "paid_session" {
			continue
		}
		bookingID, err := uuid.Parse(paged[i].ID)
		if err != nil {
			// Shouldn't happen — ID came from a db.UUID a few lines up.
			h.log.Warn("upcoming item has unparseable booking id",
				"err", err, "id", paged[i].ID)
			continue
		}
		sessionID, ok, err := h.repo.GetSessionIDForBooking(ctx, bookingID)
		if err != nil {
			h.log.Warn("failed to look up session id for booking",
				"err", err, "booking_id", paged[i].ID)
			continue
		}
		if ok {
			v := sessionID.String()
			paged[i].SessionID = &v
		}
	}

	meta := map[string]int{
		"page":        page,
		"per_page":    limit,
		"total":       total,
		"total_pages": totalPages,
	}

	c.JSON(http.StatusOK, api.NewSuccessWithMeta("Upcoming bookings retrieved", api.CodeOK, paged, meta))
}
