package routes

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	bookingsvc "github.com/hngprojects/personal-trainer-be/internal/bookings"
	"github.com/hngprojects/personal-trainer-be/internal/common"
)

func (s *routerImpl) BookDiscoveryCall(c *gin.Context) {
	if s.bookings == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	userID, ok := authenticatedUserID(c)
	if !ok {
		return
	}

	var body api.BookDiscoveryCallJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusUnprocessableEntity, api.NewError("invalid request body", api.CodeInvalidInput))
		return
	}

	input := bookingsvc.BookDiscoveryCallInput{
		TrainerID: uuid.UUID(body.TrainerId),
		ClientID:  userID,
		SlotID:    uuid.UUID(body.SlotId),
	}
	if body.Timezone != nil {
		input.Timezone = *body.Timezone
	}

	result, err := s.bookings.BookDiscoveryCall(c.Request.Context(), input)
	if err != nil {
		status, code, message, meta := mapDiscoveryBookingError(err)
		if meta != nil {
			c.JSON(status, gin.H{
				"status":  "error",
				"message": message,
				"code":    code,
				"meta":    meta,
			})
			return
		}
		c.JSON(status, api.NewError(message, code))
		return
	}

	c.JSON(http.StatusCreated, api.NewSuccess(
		"Discovery call booked successfully",
		api.CodeCreated,
		discoveryBookingDataToAPI(result),
	))
}

func authenticatedUserID(c *gin.Context) (uuid.UUID, bool) {
	userIDValue, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return uuid.Nil, false
	}

	userID, ok := userIDValue.(uuid.UUID)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("invalid authenticated user", api.CodeUnauthorized))
		return uuid.Nil, false
	}

	return userID, true
}

func discoveryBookingDataToAPI(result bookingsvc.BookDiscoveryCallResult) api.DiscoveryBookingData {
	scheduledStart := time.Now().UTC()
	if result.Booking.ScheduledStart.Valid {
		scheduledStart = result.Booking.ScheduledStart.Time
	}

	scheduledEnd := scheduledStart
	if result.Booking.ScheduledEnd.Valid {
		scheduledEnd = result.Booking.ScheduledEnd.Time
	}

	timezone := result.Slot.Timezone
	if result.Booking.Timezone.Valid && result.Booking.Timezone.String != "" {
		timezone = result.Booking.Timezone.String
	}

	var meetingID *string
	if result.Meeting.MeetingID != "" {
		meetingID = &result.Meeting.MeetingID
	}

	return api.DiscoveryBookingData{
		BookingId:       openapi_types.UUID(result.Booking.ID),
		TrainerId:       openapi_types.UUID(result.Booking.TrainerID),
		ClientId:        openapi_types.UUID(result.Booking.ClientID),
		SlotId:          openapi_types.UUID(result.Slot.ID),
		MeetingId:       meetingID,
		ScheduledStart:  scheduledStart,
		ScheduledEnd:    scheduledEnd,
		Timezone:        timezone,
		BookingStatus:   nullStringValue(result.Booking.BookingStatus),
		SessionPlatform: nullStringValue(result.Booking.SessionPlatform),
		MeetingJoinUrl:  nullStringValue(result.Booking.MeetingJoinUrl),
		MeetingStartUrl: nullStringValue(result.Booking.MeetingStartUrl),
		Notifications: api.DiscoveryBookingNotificationStatus{
			ClientEmailSent:  result.Notifications.ClientEmailSent,
			TrainerEmailSent: result.Notifications.TrainerEmailSent,
			Warnings:         result.Notifications.Warnings,
		},
	}
}

func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func mapDiscoveryBookingError(err error) (int, string, string, any) {
	var alreadyUsedErr *bookingsvc.DiscoveryAlreadyUsedError
	switch {
	case errors.Is(err, bookingsvc.ErrClientRoleRequired):
		return http.StatusForbidden, api.CodeForbidden, "client role required", nil
	case errors.As(err, &alreadyUsedErr):
		return http.StatusForbidden, api.CodeForbidden, "free discovery call already used", gin.H{
			"upgrade_url": alreadyUsedErr.UpgradeURL,
		}
	case errors.Is(err, bookingsvc.ErrTrainerNotFound), errors.Is(err, bookingsvc.ErrSlotNotFound):
		return http.StatusNotFound, api.CodeNotFound, "trainer or slot not found", nil
	case errors.Is(err, bookingsvc.ErrSlotUnavailable):
		return http.StatusConflict, api.CodeConflict, "requested slot is unavailable", nil
	case errors.Is(err, bookingsvc.ErrSlotMismatch):
		return http.StatusUnprocessableEntity, api.CodeInvalidInput, "slot does not belong to trainer", nil
	case errors.Is(err, bookingsvc.ErrZoomUnavailable):
		return http.StatusServiceUnavailable, api.CodeServerError, "meeting provider unavailable", nil
	case errors.Is(err, bookingsvc.ErrClientNotFound):
		return http.StatusUnauthorized, api.CodeUnauthorized, "client not found", nil
	default:
		return http.StatusInternalServerError, api.CodeInternalError, "internal server error", nil
	}
}
