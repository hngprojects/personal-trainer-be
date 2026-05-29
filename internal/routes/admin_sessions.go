package routes

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

// AdminCancelSession handles PUT /admin/sessions/:id/cancel
func (s *routerImpl) AdminCancelSession(c *gin.Context) {
	if s.bookings == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	idStr := c.Param("id")
	bookingID, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid session id", api.CodeBadRequest))
		return
	}

	var req struct {
		Reason string `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("reason is required", api.CodeBadRequest))
		return
	}

	ctx := c.Request.Context()

	tx, err := s.bookings.db.BeginTx(ctx, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to start transaction", api.CodeServerError))
		return
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.bookings.q.WithTx(tx)

	booking, err := qtx.GetBookingByIDForUpdate(ctx, bookingID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewError("session not found", api.CodeNotFound))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to fetch session", api.CodeServerError))
		return
	}

	if booking.BookingStatus.Valid && booking.BookingStatus.String == "cancelled" {
		c.JSON(http.StatusConflict, api.NewError("session is already cancelled", api.CodeConflict))
		return
	}
	if booking.BookingStatus.Valid && booking.BookingStatus.String == "completed" {
		c.JSON(http.StatusConflict, api.NewError("cannot cancel a completed session", api.CodeConflict))
		return
	}

	result, err := qtx.CancelBooking(ctx, db.CancelBookingParams{
		CancellationReason: sql.NullString{String: req.Reason, Valid: true},
		ID:                 bookingID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to cancel session", api.CodeServerError))
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to commit cancellation", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("session cancelled successfully", api.CodeOK, map[string]interface{}{
		"id":                  result.ID.String(),
		"booking_status":      result.BookingStatus.String,
		"cancellation_reason": result.CancellationReason.String,
		"cancelled_at":        result.CancelledAt.Time.Format(time.RFC3339),
	}))
}

// AdminRescheduleSession handles PUT /admin/sessions/:id/reschedule
func (s *routerImpl) AdminRescheduleSession(c *gin.Context) {
	if s.bookings == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	idStr := c.Param("id")
	bookingID, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid session id", api.CodeBadRequest))
		return
	}

	var req struct {
		ScheduledStart string `json:"scheduled_start" binding:"required"`
		ScheduledEnd   string `json:"scheduled_end" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("scheduled_start and scheduled_end are required", api.CodeBadRequest))
		return
	}

	newStart, err := time.Parse(time.RFC3339, req.ScheduledStart)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("scheduled_start must be a valid RFC3339 timestamp", api.CodeBadRequest))
		return
	}
	newEnd, err := time.Parse(time.RFC3339, req.ScheduledEnd)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("scheduled_end must be a valid RFC3339 timestamp", api.CodeBadRequest))
		return
	}
	if !newEnd.After(newStart) {
		c.JSON(http.StatusBadRequest, api.NewError("scheduled_end must be after scheduled_start", api.CodeBadRequest))
		return
	}

	ctx := c.Request.Context()

	tx, err := s.bookings.db.BeginTx(ctx, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to start transaction", api.CodeServerError))
		return
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.bookings.q.WithTx(tx)

	booking, err := qtx.GetBookingByIDForUpdate(ctx, bookingID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewError("session not found", api.CodeNotFound))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to fetch session", api.CodeServerError))
		return
	}

	if booking.BookingStatus.Valid && booking.BookingStatus.String == "cancelled" {
		c.JSON(http.StatusConflict, api.NewError("cannot reschedule a cancelled session", api.CodeConflict))
		return
	}
	if booking.BookingStatus.Valid && booking.BookingStatus.String == "completed" {
		c.JSON(http.StatusConflict, api.NewError("cannot reschedule a completed session", api.CodeConflict))
		return
	}

	result, err := qtx.AdminRescheduleBooking(ctx, db.AdminRescheduleBookingParams{
		ScheduledStart: newStart,
		ScheduledEnd:   newEnd,
		ID:             bookingID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusConflict, api.NewError("session could not be rescheduled", api.CodeConflict))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to reschedule session", api.CodeServerError))
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to commit reschedule", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("session rescheduled successfully", api.CodeOK, map[string]interface{}{
		"id":               result.ID.String(),
		"booking_status":   result.BookingStatus.String,
		"scheduled_start":  result.ScheduledStart.Time.Format(time.RFC3339),
		"scheduled_end":    result.ScheduledEnd.Time.Format(time.RFC3339),
		"reschedule_count": result.RescheduleCount,
	}))
}
