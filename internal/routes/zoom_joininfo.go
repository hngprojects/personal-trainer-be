// GET /sessions/{id}/join-info
//
// Returns everything the mobile / web SDK needs to drop a user
// straight into the Zoom meeting:
//
//   - sdk_key            (so the client can initialise the Zoom SDK)
//   - meeting_number     (Zoom's numeric ID; strip the leading 'm=' etc.)
//   - signature          (HS256 JWT minted server-side; expires in ~2h)
//   - role               (1 = host = the trainer; 0 = participant)
//   - join_url           (fallback for older app builds that haven't
//                         shipped the SDK yet — they can still open
//                         the link in Zoom proper)
//
// Auth: the caller must be either the booking's client OR the booking's
// trainer (i.e. trainer.user_id == auth user). Anyone else 403s, even
// admins, since this token would let them join the call.
package routes

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/zoom"
)

type zoomJoinInfoRoutes interface {
	register(group *gin.RouterGroup, authMw gin.HandlerFunc)
}

type zoomJoinInfoHandler struct {
	q      *db.Queries
	signer *zoom.SDKSigner // nil if SDK key not configured → handler 503s
	cfg    *config.Config
	log    *slog.Logger
}

func newZoomJoinInfoHandler(q *db.Queries, signer *zoom.SDKSigner, cfg *config.Config, log *slog.Logger) zoomJoinInfoRoutes {
	return &zoomJoinInfoHandler{q: q, signer: signer, cfg: cfg, log: log}
}

func (h *zoomJoinInfoHandler) register(group *gin.RouterGroup, authMw gin.HandlerFunc) {
	group.GET("/sessions/:id/join-info", authMw, h.joinInfo)
}

func (h *zoomJoinInfoHandler) joinInfo(c *gin.Context) {
	if !h.signer.IsConfigured() {
		c.JSON(http.StatusServiceUnavailable, api.NewError("zoom meeting SDK not configured on this server", api.CodeServerError))
		return
	}
	sessionIDStr := c.Param("id")
	sessionID, parseErr := uuid.Parse(sessionIDStr)
	if parseErr != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid session id", api.CodeBadRequest))
		return
	}
	userID, ok := common.UserIDFromContext(c.Request.Context())
	if !ok || userID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}

	session, err := h.q.GetBookingSessionById(c.Request.Context(), sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewError("session not found", api.CodeNotFound))
			return
		}
		h.log.Error("zoom join-info: load session failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal error", api.CodeServerError))
		return
	}

	booking, err := h.q.GetBookingByID(c.Request.Context(), session.BookingID)
	if err != nil {
		h.log.Error("zoom join-info: load booking failed", "err", err, "session_id", sessionID)
		c.JSON(http.StatusInternalServerError, api.NewError("internal error", api.CodeServerError))
		return
	}

	if !booking.ZoomMeetingID.Valid || booking.ZoomMeetingID.String == "" {
		// Booking exists but never got a Zoom meeting — typically a
		// non-zoom session_platform. Surface clearly so the mobile app
		// can show "this session isn't a Zoom call" instead of a
		// confusing crash.
		c.JSON(http.StatusBadRequest, api.NewError("this session has no zoom meeting attached", api.CodeBadRequest))
		return
	}

	// Authorisation: caller must be the client OR the trainer-as-user.
	// trainer_id on the session row is trainer record PK; need user_id
	// to compare against the auth user. Single small JOIN — fine on hot
	// path.
	trainerRow, err := h.q.GetTrainerUserDetails(c.Request.Context(), session.TrainerID)
	if err != nil {
		h.log.Error("zoom join-info: load trainer details failed", "err", err, "trainer_id", session.TrainerID)
		c.JSON(http.StatusInternalServerError, api.NewError("internal error", api.CodeServerError))
		return
	}

	var role zoom.SDKRole
	switch userID {
	case booking.ClientID:
		role = zoom.SDKRoleParticipant
	case trainerRow.ID:
		role = zoom.SDKRoleHost
	default:
		c.JSON(http.StatusForbidden, api.NewError("you are not a participant of this session", api.CodeForbidden))
		return
	}

	sig, err := h.signer.Sign(booking.ZoomMeetingID.String, role, 0) // 0 = default 2h
	if err != nil {
		h.log.Error("zoom join-info: sign failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to sign meeting token", api.CodeServerError))
		return
	}

	joinURL := ""
	if booking.ZoomMeetingLink.Valid {
		joinURL = booking.ZoomMeetingLink.String
	}

	c.JSON(http.StatusOK, api.NewSuccess("ok", api.CodeOK, gin.H{
		"sdk_key":        h.cfg.ZoomSDKKey,
		"meeting_number": booking.ZoomMeetingID.String,
		"signature":      sig,
		"role":           int(role),
		"join_url":       joinURL,
	}))
}
