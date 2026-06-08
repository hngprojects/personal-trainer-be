package bookings

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
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/internal/notification"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
	"github.com/hngprojects/personal-trainer-be/pkg/meeting"
)

const maxReschedules = 3
const lockWindowHours = 12

type Handler struct {
	repo Repository
	// meetings is the per-trainer-aware Selector; see meeting.Selector.
	// For reschedules we always own the trainer ID (booking.TrainerID),
	// so we can route the new meeting create + the old meeting delete
	// through whichever provider that trainer hosts on.
	meetings meeting.Selector
	mailer   email.Mailer
	log      *slog.Logger
	// joinLinks must mirror the one wired into the booking service so
	// reschedule-confirmation emails get the same SDK-vs-link treatment
	// the initial confirmation got.
	joinLinks JoinLinkBuilder
	// orgFallback is the org meeting Provider used to clean up old
	// meetings the per-trainer provider doesn't recognise. Optional —
	// nil is fine on local dev, but in production it's the safety net
	// for trainers who flipped their hosting state mid-booking.
	orgFallback meeting.Provider
	notif       *notification.NotificationService
}

// deleteWithOrgFallback attempts to delete a Zoom meeting via the
// caller-supplied provider; on failure, retries once against the org
// provider before logging for manual cleanup. The org retry covers the
// case where a booking was hosted under one account (e.g. org) but the
// trainer has since connected their own Zoom, so the current per-user
// provider returns 401/404 for a meeting it never owned.
func (h *Handler) deleteWithOrgFallback(ctx context.Context, prov meeting.Provider, meetingID string) {
	delErr := prov.DeleteMeeting(ctx, meetingID)
	if delErr == nil {
		return
	}
	if h.orgFallback != nil && prov != h.orgFallback {
		if delErr2 := h.orgFallback.DeleteMeeting(ctx, meetingID); delErr2 == nil {
			h.log.Info("zoom meeting cleaned up via org fallback after primary provider failed",
				"meeting_id", meetingID, "primary_err", delErr)
			return
		}
	}
	h.log.Warn("failed to delete zoom meeting — may require manual cleanup",
		"meeting_id", meetingID, "err", delErr)
}

func NewHandler(repo Repository, meetingSelector meeting.Selector, mailer email.Mailer, log *slog.Logger, joinMode, universalLinkDomain string, orgFallback meeting.Provider, notif *notification.NotificationService) *Handler {
	return &Handler{
		repo:        repo,
		meetings:    meetingSelector,
		mailer:      mailer,
		log:         log,
		joinLinks:   JoinLinkBuilder{JoinMode: joinMode, UniversalLinkDomain: universalLinkDomain},
		orgFallback: orgFallback,
		notif:       notif,
	}
}

// TryReschedulePaidSession handles PUT /bookings/{id}/reschedule for paid training sessions.
// It returns false (without writing a response) if the booking ID does not exist in the
// paid-sessions table, allowing the caller to fall back to the discovery-call handler.
// It returns true for all other outcomes (success or error), having written the response itself.
func (h *Handler) TryReschedulePaidSession(c *gin.Context, id openapi_types.UUID) bool {
	bookingID := uuid.UUID(id)
	ctx := c.Request.Context()

	booking, err := h.repo.GetBookingByID(ctx, bookingID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false
		}
		h.log.Error("failed to get booking", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return true
	}

	userIDVal, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		h.log.Warn("reschedule paid session: missing authenticated user", "bookingID", bookingID.String())
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return true
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		h.log.Warn("reschedule paid session: invalid user id in context", "bookingID", bookingID.String())
		c.JSON(http.StatusUnauthorized, api.NewError("invalid user id", api.CodeUnauthorized))
		return true
	}

	// ownership check before parsing body — avoids decoding for unauthorized callers
	if booking.ClientID != userID {
		h.log.Warn("reschedule paid session: ownership mismatch", "bookingID", bookingID.String(), "userID", userID.String())
		c.JSON(http.StatusForbidden, api.NewError("you do not have permission to reschedule this booking", api.CodeForbidden))
		return true
	}

	var req api.RescheduleBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Warn("reschedule paid session: invalid request body", "bookingID", bookingID.String(), "userID", userID.String(), "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return true
	}

	if !req.Reason.Valid() {
		h.log.Warn("reschedule paid session: invalid reason", "bookingID", bookingID.String(), "userID", userID.String())
		c.JSON(http.StatusBadRequest, api.NewError("invalid reason", api.CodeBadRequest))
		return true
	}

	if _, err := time.LoadLocation(req.Timezone); err != nil {
		h.log.Warn("reschedule paid session: invalid timezone", "bookingID", bookingID.String(), "userID", userID.String(), "timezone", req.Timezone, "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid timezone: must be a valid IANA timezone (e.g. America/New_York)", api.CodeBadRequest))
		return true
	}

	if booking.BookingStatus.Valid {
		switch booking.BookingStatus.String {
		case "cancelled":
			h.log.Warn("reschedule paid session: booking is cancelled", "bookingID", bookingID.String(), "userID", userID.String())
			c.JSON(http.StatusForbidden, api.NewError("cannot reschedule a cancelled booking", api.CodeForbidden))
			return true
		case "completed":
			h.log.Warn("reschedule paid session: booking is completed", "bookingID", bookingID.String(), "userID", userID.String())
			c.JSON(http.StatusForbidden, api.NewError("cannot reschedule a completed booking", api.CodeForbidden))
			return true
		}
	}

	if !booking.ScheduledStart.Valid {
		h.log.Warn("reschedule paid session: no scheduled start time", "bookingID", bookingID.String(), "userID", userID.String())
		c.JSON(http.StatusBadRequest, api.NewError("booking has no scheduled start time", api.CodeBadRequest))
		return true
	}

	now := time.Now().UTC()
	lockDeadline := booking.ScheduledStart.Time.UTC().Add(-lockWindowHours * time.Hour)
	if !now.Before(lockDeadline) {
		h.log.Warn("reschedule paid session: within lock window", "bookingID", bookingID.String(), "userID", userID.String(), "scheduledStart", booking.ScheduledStart.Time)
		c.JSON(http.StatusForbidden, api.NewError(
			fmt.Sprintf("rescheduling is not allowed within %d hours of the session start time", lockWindowHours),
			api.CodeForbidden,
		))
		return true
	}

	if booking.RescheduleCount >= maxReschedules {
		h.log.Warn("reschedule paid session: max reschedule limit reached", "bookingID", bookingID.String(), "userID", userID.String(), "rescheduleCount", booking.RescheduleCount)
		c.JSON(http.StatusTooManyRequests, api.NewError(
			fmt.Sprintf("maximum reschedule limit of %d has been reached for this booking", maxReschedules),
			api.CodeTooManyRequests,
		))
		return true
	}

	newStart := req.NewDatetime.UTC()
	if newStart.Before(now) {
		h.log.Warn("reschedule paid session: new time is in the past", "bookingID", bookingID.String(), "userID", userID.String(), "newStart", newStart)
		c.JSON(http.StatusBadRequest, api.NewError("new time is in the past", api.CodeBadRequest))
		return true
	}

	var newEnd time.Time
	if booking.ScheduledEnd.Valid {
		duration := booking.ScheduledEnd.Time.Sub(booking.ScheduledStart.Time)
		newEnd = newStart.Add(duration)
	} else {
		newEnd = newStart.Add(60 * time.Minute)
	}

	conflictCount, err := h.repo.CheckPaidBookingConflict(ctx, db.CheckPaidBookingConflictParams{
		TrainerID: booking.TrainerID,
		ExcludeID: bookingID,
		NewStart:  newStart,
		NewEnd:    newEnd,
	})
	if err != nil {
		h.log.Error("failed to check booking conflict", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return true
	}
	if conflictCount > 0 {
		h.log.Warn("reschedule paid session: trainer unavailable", "bookingID", bookingID.String(), "userID", userID.String(), "newStart", newStart, "trainerID", booking.TrainerID)
		c.JSON(http.StatusConflict, api.NewError("the trainer is unavailable at the requested time", api.CodeConflict))
		return true
	}

	// fetch client once — reused for both zoom topic and confirmation email
	clientUser, clientErr := h.repo.GetUserByID(ctx, booking.ClientID)
	if clientErr != nil {
		h.log.Warn("failed to fetch client", "err", clientErr)
	}

	// Look up the trainer's user_id once — both create and delete need
	// to route through the same Zoom provider (per-trainer if connected,
	// org otherwise), and that selection is keyed on user_id. A failure
	// here is non-fatal: we fall back to uuid.Nil → org provider.
	var trainerUserID uuid.UUID
	if trainerRow, tErr := h.repo.GetTrainerDetails(ctx, booking.TrainerID); tErr == nil {
		trainerUserID = trainerRow.ID
	} else {
		h.log.Warn("failed to resolve trainer user_id for meeting provider — falling back to org", "trainer_id", booking.TrainerID, "err", tErr)
	}
	// Reschedule reuses whatever platform the original booking was on
	// (a paid booking can't change platform mid-life). Default to zoom
	// when the column is somehow blank — the existing column has a
	// `DEFAULT 'zoom'` at the schema level so this is just defence in
	// depth.
	platform := booking.SessionPlatform.String
	if platform == "" {
		platform = meeting.PlatformZoom
	}
	meetingProv := h.meetings.For(ctx, trainerUserID, platform)

	newZoomLink := booking.ZoomMeetingLink
	newZoomMeetingID := booking.ZoomMeetingID
	oldZoomMeetingID := ""

	if booking.ZoomMeetingID.Valid && booking.ZoomMeetingID.String != "" {
		topic := "Training Session"
		if clientErr == nil && clientUser.Name != "" {
			topic = "Training Session with " + clientUser.Name
		}
		durationMins := int(newEnd.Sub(newStart).Minutes())
		link, meetID, zoomErr := meetingProv.CreateMeeting(ctx, topic, newStart, durationMins)
		if zoomErr != nil {
			h.log.Warn("failed to create new zoom meeting for reschedule — keeping old link", "err", zoomErr)
		} else {
			oldZoomMeetingID = booking.ZoomMeetingID.String
			newZoomLink = sql.NullString{String: link, Valid: true}
			newZoomMeetingID = sql.NullString{String: meetID, Valid: true}
		}
	}

	oldStart := booking.ScheduledStart.Time

	updated, err := h.repo.ReschedulePaidBooking(ctx, db.ReschedulePaidBookingParams{
		ID:              bookingID,
		ScheduledStart:  newStart,
		ScheduledEnd:    newEnd,
		ZoomMeetingLink: newZoomLink,
		ZoomMeetingID:   newZoomMeetingID,
	})
	if err != nil {
		if newZoomMeetingID.Valid && newZoomMeetingID.String != oldZoomMeetingID {
			cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			h.deleteWithOrgFallback(cleanCtx, meetingProv, newZoomMeetingID.String)
			cancel()
		}
		if errors.Is(err, sql.ErrNoRows) {
			h.log.Warn("booking concurrently cancelled or max reschedules reached", "booking_id", bookingID)
			c.JSON(http.StatusConflict, api.NewError("booking was concurrently modified — please refresh and try again", api.CodeConflict))
			return true
		}
		h.log.Error("failed to reschedule paid booking", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to reschedule booking", api.CodeServerError))
		return true
	}

	if oldZoomMeetingID != "" {
		delCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		h.deleteWithOrgFallback(delCtx, meetingProv, oldZoomMeetingID)
		cancel()
	}

	if err := h.repo.CreatePaidRescheduleHistory(ctx, db.CreatePaidRescheduleHistoryParams{
		BookingID:     bookingID,
		PreviousStart: oldStart,
		NewStart:      newStart,
		Reason:        sql.NullString{String: string(req.Reason), Valid: true},
	}); err != nil {
		h.log.Error("failed to record paid reschedule history", "err", err)
	}

	finalZoomLink := ""
	if updated.ZoomMeetingLink.Valid {
		finalZoomLink = updated.ZoomMeetingLink.String
	}
	// Resolve the session id so the email's "Join" target matches the
	// universal-link/raw-URL choice the initial confirmation used.
	// Lookup is non-fatal: on failure the builder gets uuid.Nil and
	// silently falls back to the raw Zoom URL.
	var sessionID uuid.UUID
	if sess, sErr := h.repo.GetBookingSessionByBookingID(ctx, bookingID); sErr == nil {
		sessionID = sess.ID
	}
	finalJoinLink := h.joinLinks.Build(finalZoomLink, sessionID)

	if clientErr == nil {
		if err := h.mailer.SendPaidSessionRescheduleConfirmation(
			clientUser.Email, clientUser.Name, oldStart, newStart, req.Timezone, finalJoinLink,
		); err != nil {
			h.log.Error("failed to send reschedule confirmation email to client", "err", err)
		}
	}

	trainer, trainerErr := h.repo.GetTrainerByID(ctx, booking.TrainerID)
	if trainerErr == nil {
		trainerUser, tuErr := h.repo.GetUserByID(ctx, trainer.UserID)
		if tuErr == nil {
			clientName := "client"
			if clientErr == nil {
				clientName = clientUser.Name
			}
			if err := h.mailer.SendPaidSessionRescheduleTrainerNotification(
				trainerUser.Email, clientName, oldStart, newStart, req.Timezone, finalJoinLink,
			); err != nil {
				h.log.Error("failed to send reschedule notification email to trainer", "err", err)
			}
			// Notify trainer about reschedule
			if h.notif != nil {
				if _, notifErr := h.notif.SendNotificationToUser(ctx, trainerUser.ID,
					"Session Rescheduled",
					clientName+" has rescheduled their session.",
					"reschedule-"+bookingID.String(),
				); notifErr != nil {
					h.log.Warn("reschedule notification to trainer failed", "trainerID", booking.TrainerID, "err", notifErr)
				}
			}
		}
	}

	c.JSON(http.StatusOK, api.NewSuccess("Booking rescheduled successfully", api.CodeOK, bookingToResponse(updated)))
	return true
}

func bookingToResponse(b db.Booking) map[string]interface{} {
	resp := map[string]interface{}{
		"id":                b.ID,
		"trainer_id":        b.TrainerID,
		"client_id":         b.ClientID,
		"reschedule_count":  b.RescheduleCount,
		"status":            nil,
		"session_platform":  nil,
		"scheduled_start":   nil,
		"scheduled_end":     nil,
		"timezone":          nil,
		"zoom_meeting_link": nil,
	}
	if b.BookingStatus.Valid {
		resp["status"] = b.BookingStatus.String
	}
	if b.SessionPlatform.Valid {
		resp["session_platform"] = b.SessionPlatform.String
	}
	if b.ScheduledStart.Valid {
		resp["scheduled_start"] = b.ScheduledStart.Time
	}
	if b.ScheduledEnd.Valid {
		resp["scheduled_end"] = b.ScheduledEnd.Time
	}
	if b.Timezone.Valid {
		resp["timezone"] = b.Timezone.String
	}
	if b.ZoomMeetingLink.Valid {
		resp["zoom_meeting_link"] = b.ZoomMeetingLink.String
	}
	return resp
}
