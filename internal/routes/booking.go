package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/api"
)

func (s *routerImpl) GetTrainersBookingSlots(c *gin.Context, trainerId uuid.UUID) {
	if s.bookingSlot == nil {
		s.logger.Warn("GetTrainersBookingSlots: booking slot handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.bookingSlot.HandleGetTrainersBookingSlots(c, trainerId)
}

func (s *routerImpl) CreateBooking(c *gin.Context) {
	if s.booking == nil {
		s.logger.Warn("CreateBooking: booking handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.booking.HandleCreateBookingSession(c)
}
