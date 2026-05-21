package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/api"
)

func (s *routerImpl) HandleGetSessionById(c *gin.Context, id uuid.UUID) {
	if s.bookingSession == nil {
		s.logger.Warn("HandleGetSessionById: booking session handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.bookingSession.HandleGetSessionById(c, id)
}

func (s *routerImpl) HandleStartSession(c *gin.Context, id uuid.UUID) {
	if s.bookingSession == nil {
		s.logger.Warn("HandleStartSession: booking session handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.bookingSession.StartSessionHandler(c, id)
}

func (s *routerImpl) HandleJoinSession(c *gin.Context, id uuid.UUID) {
	if s.bookingSession == nil {
		s.logger.Warn("HandleJoinSession: booking session handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.bookingSession.JoinSessionHandler(c, id)
}

func (s *routerImpl) HandleCompleteSession(c *gin.Context, id uuid.UUID) {
	if s.bookingSession == nil {
		s.logger.Warn("HandleCompleteSession: booking session handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.bookingSession.CompleteSession(c, id)
}

func (s *routerImpl) HandleTrainersNote(c *gin.Context, id uuid.UUID) {
	if s.bookingSession == nil {
		s.logger.Warn("HandleTrainersNote: booking session handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.bookingSession.TrainersNote(c, id)
}
