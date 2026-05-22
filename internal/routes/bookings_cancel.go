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
	"github.com/hngprojects/personal-trainer-be/internal/common"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type bookingsStore struct {
	db *sql.DB
	q  *db.Queries
}

// CancelBooking handles PUT /bookings/:id/cancel
// Allows clients or trainers to cancel confirmed bookings with refund calculation
func (s *routerImpl) CancelBooking(c *gin.Context, id uuid.UUID) {
	if s.bookings == nil {
		s.logger.Warn("cancel booking: bookings store not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	// Extract authenticated user ID from context
	userIDVal, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		s.logger.Warn("cancel booking: missing authenticated user", "bookingID", id.String())
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		s.logger.Warn("cancel booking: invalid user id in context", "bookingID", id.String())
		c.JSON(http.StatusUnauthorized, api.NewError("invalid user id", api.CodeUnauthorized))
		return
	}

	// Parse request body
	var req api.CancelBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.Warn("cancel booking: invalid request body", "bookingID", id.String(), "userID", userID.String(), "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	// Validate cancellation reason is one of the allowed enum values
	validReasons := map[string]bool{
		"schedule_conflict":  true,
		"feeling_unwell":     true,
		"personal_emergency": true,
		"trainer_request":    true,
		"client_request":     true,
		"other":              true,
	}
	if req.Reason == "" || !validReasons[string(req.Reason)] {
		s.logger.Warn("cancel booking: invalid cancellation reason", "bookingID", id.String(), "userID", userID.String(), "reason", req.Reason)
		c.JSON(http.StatusBadRequest, api.NewError("invalid cancellation reason", api.CodeBadRequest))
		return
	}

	ctx := c.Request.Context()

	// Start transaction with deferred rollback
	tx, err := s.bookings.db.BeginTx(ctx, nil)
	if err != nil {
		s.logger.Warn("failed to cancel booking request", "userID", userID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to start transaction", api.CodeServerError))
		return
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.bookings.q.WithTx(tx)

	// Get booking with FOR UPDATE lock to prevent concurrent cancellations
	booking, err := qtx.GetBookingByIDForUpdate(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.logger.Warn("cancel booking: booking not found", "bookingID", id.String(), "userID", userID.String(), "err", err)
			c.JSON(http.StatusNotFound, api.NewNotFoundError("booking"))
			return
		}
		s.logger.Warn("cancel booking: failed to load booking", "bookingID", id.String(), "userID", userID.String(), "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load booking", api.CodeServerError))
		return
	}

	// Verify user is either the client or trainer of this booking
	if booking.ClientID != userID && booking.TrainerID != userID {
		s.logger.Warn("cancel booking: user not authorized", "bookingID", booking.ID.String(), "userID", userID.String())
		c.JSON(http.StatusForbidden, api.NewError("you are not authorized to cancel this booking", api.CodeForbidden))
		return
	}

	// Check if booking is already cancelled
	if booking.BookingStatus.Valid && booking.BookingStatus.String == "cancelled" {
		s.logger.Warn("cancel booking: already cancelled", "bookingID", booking.ID.String(), "userID", userID.String())
		c.JSON(http.StatusConflict, api.NewError("booking is already cancelled", api.CodeConflict))
		return
	}

	// Check if booking is confirmed
	if !booking.BookingStatus.Valid || booking.BookingStatus.String != "confirmed" {
		s.logger.Warn("cancel booking: booking not confirmed", "bookingID", booking.ID.String(), "userID", userID.String(), "status", booking.BookingStatus.String)
		c.JSON(http.StatusConflict, api.NewError("only confirmed bookings can be cancelled", api.CodeConflict))
		return
	}

	// Check if session has already started (in UTC)
	now := time.Now().UTC()
	if booking.ScheduledStart.Valid && now.After(booking.ScheduledStart.Time) {
		s.logger.Warn("cancel booking: session already started", "bookingID", booking.ID.String(), "userID", userID.String(), "scheduledStart", booking.ScheduledStart.Time)
		c.JSON(http.StatusConflict, api.NewError("cannot cancel a session that has already started", api.CodeConflict))
		return
	}

	// Calculate refund amount based on cancellation window (12 hours)
	refundAmount := 0
	refundReason := api.Within12Hours

	if booking.ScheduledStart.Valid {
		hoursUntilSession := booking.ScheduledStart.Time.Sub(now).Hours()
		if hoursUntilSession > 12 {
			refundAmount = 1
			refundReason = api.After12Hours
		}
	}

	// Prepare cancellation reason
	cancellationReason := string(req.Reason)
	if req.Notes != nil && *req.Notes != "" {
		cancellationReason += " - " + *req.Notes
	}

	// Cancel the booking
	cancelledBooking, err := qtx.CancelBooking(ctx, db.CancelBookingParams{
		ID:                 id,
		CancellationReason: sql.NullString{String: cancellationReason, Valid: true},
	})
	if err != nil {
		s.logger.Warn("failed to cancel booking", "userID", userID, "booking", booking.ID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to cancel booking", api.CodeServerError))
		return
	}

	// Release the booking slot back to availability
	if booking.ScheduledStart.Valid && booking.ScheduledEnd.Valid {
		// ReleaseBookingSlotParams.TrainerID is uuid.NullUUID (sqlc inferred
		// the column as nullable for this query path); booking.TrainerID is
		// uuid.UUID. Wrap with Valid=true — the FK guarantees the booking has
		// a non-null trainer.
		rowsAffected, err := qtx.ReleaseBookingSlot(ctx, db.ReleaseBookingSlotParams{
			TrainerID:      uuid.NullUUID{UUID: booking.TrainerID, Valid: true},
			ScheduledStart: booking.ScheduledStart.Time,
			ScheduledEnd:   booking.ScheduledEnd.Time,
			Timezone:       booking.Timezone.String,
		})
		if err != nil {
			s.logger.Warn("failed to release booking slot", "userID", userID, "err", err)
			c.JSON(http.StatusInternalServerError, api.NewError("failed to release booking slot", api.CodeServerError))
			return
		}
		if rowsAffected != 1 {
			s.logger.Warn("failed to release booking slot: unexpected rows affected", "userID", userID, "bookingID", booking.ID, "rowsAffected", rowsAffected)
			c.JSON(http.StatusInternalServerError, api.NewError("failed to release booking slot: slot not found", api.CodeServerError))
			return
		}
	}

	// Refund credits if applicable (only for active subscriptions)
	if refundAmount > 0 && booking.SubscriptionID.Valid {
		subscription, err := qtx.GetSubscriptionByID(ctx, booking.SubscriptionID.UUID)
		if err != nil {
			s.logger.Warn("cancel booking: get subscription failed", "bookingID", booking.ID.String(), "userID", userID.String(), "subscriptionID", booking.SubscriptionID.UUID, "err", err)
			if errors.Is(err, sql.ErrNoRows) {
				c.JSON(http.StatusInternalServerError, api.NewError("subscription not found", api.CodeServerError))
				return
			}
			c.JSON(http.StatusInternalServerError, api.NewError("failed to verify subscription", api.CodeServerError))
			return
		}

		// Only refund if subscription is active
		if subscription.Status == "active" {
			_, err = qtx.RefundSessionCredit(ctx, booking.SubscriptionID.UUID)
			if err != nil {
				s.logger.Warn("failed to refund credit for active subscriptions", "userID", userID, "subscriptionID", booking.SubscriptionID.UUID, "err", err)
				c.JSON(http.StatusInternalServerError, api.NewError("failed to refund credits", api.CodeServerError))
				return
			}
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		s.logger.Warn("cancel booking request transaction failed", "userID", userID, "bookingID", booking.ID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to commit transaction", api.CodeServerError))
		return
	}

	// Build response
	response := api.CancelBookingResponse{
		Status:  api.CancelBookingResponseStatusSuccess,
		Code:    "OK",
		Message: "booking cancelled successfully",
		Data: &struct {
			BookingId        openapi_types.UUID                        `json:"booking_id"`
			CancelledAt      time.Time                                 `json:"cancelled_at"`
			NotificationSent bool                                      `json:"notification_sent"`
			RefundAmount     int                                       `json:"refund_amount"`
			RefundReason     api.CancelBookingResponseDataRefundReason `json:"refund_reason"`
			Status           string                                    `json:"status"`
		}{
			BookingId:        openapi_types.UUID(cancelledBooking.ID),
			CancelledAt:      cancelledBooking.CancelledAt.Time,
			RefundAmount:     refundAmount,
			RefundReason:     refundReason,
			Status:           "cancelled",
			NotificationSent: true,
		},
	}

	c.JSON(http.StatusOK, response)
}
