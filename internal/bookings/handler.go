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
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
	"github.com/hngprojects/personal-trainer-be/pkg/meeting"
)

const maxReschedules = 3
const lockWindowHours = 12

type Handler struct {
	repo    Repository
	meeting meeting.Provider
	mailer  email.Mailer
	log     *slog.Logger
}

func NewHandler(repo Repository, meetingProvider meeting.Provider, mailer email.Mailer, log *slog.Logger) *Handler {
	return &Handler{repo: repo, meeting: meetingProvider, mailer: mailer, log: log}
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
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return true
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("invalid user id", api.CodeUnauthorized))
		return true
	}

	// ownership check before parsing body — avoids decoding for unauthorized callers
	if booking.ClientID != userID {
		c.JSON(http.StatusForbidden, api.NewError("you do not have permission to reschedule this booking", api.CodeForbidden))
		return true
	}

	var req api.RescheduleBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return true
	}

	if !req.Reason.Valid() {
		c.JSON(http.StatusBadRequest, api.NewError("invalid reason", api.CodeBadRequest))
		return true
	}

	if _, err := time.LoadLocation(req.Timezone); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid timezone: must be a valid IANA timezone (e.g. America/New_York)", api.CodeBadRequest))
		return true
	}

	if booking.BookingStatus.Valid {
		switch booking.BookingStatus.String {
		case "cancelled":
			c.JSON(http.StatusForbidden, api.NewError("cannot reschedule a cancelled booking", api.CodeForbidden))
			return true
		case "completed":
			c.JSON(http.StatusForbidden, api.NewError("cannot reschedule a completed booking", api.CodeForbidden))
			return true
		}
	}

	if !booking.ScheduledStart.Valid {
		c.JSON(http.StatusBadRequest, api.NewError("booking has no scheduled start time", api.CodeBadRequest))
		return true
	}

	now := time.Now().UTC()
	lockDeadline := booking.ScheduledStart.Time.UTC().Add(-lockWindowHours * time.Hour)
	if !now.Before(lockDeadline) {
		c.JSON(http.StatusForbidden, api.NewError(
			fmt.Sprintf("rescheduling is not allowed within %d hours of the session start time", lockWindowHours),
			api.CodeForbidden,
		))
		return true
	}

	if booking.RescheduleCount >= maxReschedules {
		c.JSON(http.StatusTooManyRequests, api.NewError(
			fmt.Sprintf("maximum reschedule limit of %d has been reached for this booking", maxReschedules),
			api.CodeTooManyRequests,
		))
		return true
	}

	newStart := req.NewDatetime.UTC()
	if newStart.Before(now) {
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
		c.JSON(http.StatusConflict, api.NewError("the trainer is unavailable at the requested time", api.CodeConflict))
		return true
	}

	// fetch client once — reused for both zoom topic and confirmation email
	clientUser, clientErr := h.repo.GetUserByID(ctx, booking.ClientID)
	if clientErr != nil {
		h.log.Warn("failed to fetch client", "err", clientErr)
	}

	newZoomLink := booking.ZoomMeetingLink
	newZoomMeetingID := booking.ZoomMeetingID
	oldZoomMeetingID := ""

	if booking.ZoomMeetingID.Valid && booking.ZoomMeetingID.String != "" {
		topic := "Training Session"
		if clientErr == nil && clientUser.Name != "" {
			topic = "Training Session with " + clientUser.Name
		}
		durationMins := int(newEnd.Sub(newStart).Minutes())
		link, meetID, _, zoomErr := h.meeting.CreateMeeting(ctx, topic, newStart, durationMins)
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
			if delErr := h.meeting.DeleteMeeting(cleanCtx, newZoomMeetingID.String); delErr != nil {
				h.log.Error("orphaned zoom meeting after DB failure — manual cleanup required",
					"meeting_id", newZoomMeetingID.String, "err", delErr)
			}
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
		if delErr := h.meeting.DeleteMeeting(delCtx, oldZoomMeetingID); delErr != nil {
			h.log.Warn("failed to delete old zoom meeting — may require manual cleanup",
				"meeting_id", oldZoomMeetingID, "err", delErr)
		}
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

	if clientErr == nil {
		if err := h.mailer.SendPaidSessionRescheduleConfirmation(
			clientUser.Email, clientUser.Name, oldStart, newStart, req.Timezone, finalZoomLink,
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
				trainerUser.Email, clientName, oldStart, newStart, req.Timezone, finalZoomLink,
			); err != nil {
				h.log.Error("failed to send reschedule notification email to trainer", "err", err)
			}
		}
	}

	c.JSON(http.StatusOK, api.NewSuccess("Booking rescheduled successfully", api.CodeOK, bookingToResponse(updated)))
	return true
}

func bookingToResponse(b db.ReschedulePaidBookingRow) map[string]interface{} {
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
