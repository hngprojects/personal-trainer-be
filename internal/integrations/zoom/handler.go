package zoom

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/api"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// CreateZoomMeeting handles POST /integrations/zoom/create-meeting
func (h *Handler) CreateZoomMeeting(c *gin.Context) {
	var req api.CreateZoomMeetingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	if !req.BookingType.Valid() {
		c.JSON(http.StatusBadRequest, api.NewError("booking_type must be 'discovery' or 'paid_session'", api.CodeBadRequest))
		return
	}

	ctx := c.Request.Context()
	bookingID := uuid.UUID(req.BookingId)
	if bookingID == uuid.Nil {
		c.JSON(http.StatusBadRequest, api.NewError("booking_id is required", api.CodeBadRequest))
		return
	}

	var result *MeetingResult
	var err error

	switch req.BookingType {
	case api.CreateZoomMeetingRequestBookingTypeDiscovery:
		result, err = h.svc.EnsureDiscoveryMeeting(ctx, bookingID)
	case api.CreateZoomMeetingRequestBookingTypePaidSession:
		result, err = h.svc.EnsurePaidSessionMeeting(ctx, bookingID)
	default:
		c.JSON(http.StatusBadRequest, api.NewError("unsupported booking_type", api.CodeBadRequest))
		return
	}

	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			c.JSON(http.StatusNotFound, api.NewError("booking not found", api.CodeNotFound))
		case errors.Is(err, ErrNoScheduledStart):
			c.JSON(http.StatusBadRequest, api.NewError("booking has no scheduled start time", api.CodeBadRequest))
		case errors.Is(err, ErrNotConfigured):
			c.JSON(http.StatusServiceUnavailable, api.NewError("zoom integration is not configured", api.CodeServerError))
		default:
			c.JSON(http.StatusInternalServerError, api.NewError("failed to create zoom meeting", api.CodeServerError))
		}
		return
	}

	data := map[string]interface{}{
		"booking_id": bookingID,
		"join_url":   result.JoinURL,
		"meeting_id": result.MeetingID,
		"passcode":   result.Passcode,
		"existing":   result.Existing,
	}

	if result.Existing {
		c.JSON(http.StatusOK, api.NewSuccess("zoom meeting already exists", api.CodeOK, data))
		return
	}
	c.JSON(http.StatusCreated, api.NewSuccess("zoom meeting created", api.CodeCreated, data))
}
