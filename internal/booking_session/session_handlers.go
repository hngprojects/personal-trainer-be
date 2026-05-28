package booking_session

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/redis"
)

// Notifier is satisfied by notification.NotificationService.
type Notifier interface {
	SendNotificationToUser(ctx context.Context, userID uuid.UUID, title, message, idempotencyKey string) error
}

type sessionHandler struct {
	service  SessionInterface
	redis    redis.Client
	notifier Notifier
	mailer   email.Mailer
	log      *slog.Logger
}

type SessionHandler interface {
	HandleGetSessionById(c *gin.Context, sessionID uuid.UUID)
	StartSessionHandler(c *gin.Context, sessionID uuid.UUID)
	JoinSessionHandler(c *gin.Context, sessionID uuid.UUID)
	CompleteSession(c *gin.Context, sessionID uuid.UUID)
	TrainersNote(c *gin.Context, sessionID uuid.UUID)
}

func NewSessionHandler(service SessionInterface, redis redis.Client, notifier Notifier, mailer email.Mailer, log *slog.Logger) *sessionHandler {
	return &sessionHandler{service: service, redis: redis, notifier: notifier, mailer: mailer, log: log}
}

func (h *sessionHandler) HandleGetSessionById(c *gin.Context, sessionID uuid.UUID) {
	cachedKey := "session:" + sessionID.String()
	cached := h.redis.Get(c.Request.Context(), cachedKey)
	if cached.Err() == nil {
		h.log.Info("HandleGetSessionById: cache hit", "session_id", sessionID)
		var body db.GetBookingSessionByIdRow
		if err := json.Unmarshal([]byte(cached.Val()), &body); err != nil {
			h.log.Warn("HandleGetSessionById: failed to unmarshal cached data, falling back to DB", "session_id", sessionID, "err", err)
		} else {
			result := ParseResponseWithTrainer(&body)
			c.JSON(http.StatusOK, api.NewSuccessResponse("session fetched successfully", api.CodeOK, result, nil))
			return
		}
	} else {
		h.log.Warn("HandleGetSessionById: redis error: could not fetch session", "session_id", sessionID, "err", cached.Err())
	}
	session, err := h.service.GetSessionById(c.Request.Context(), sessionID)
	if err != nil {
		h.log.Warn("HandleGetSessionById: failed to fetch session", "session_id", sessionID, "err", err)
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, api.NewErrorResponse("failed to get session data", api.CodeNotFound, nil))
			return
		} else {
			c.JSON(http.StatusInternalServerError, api.NewErrorResponse("failed to get session data", api.CodeServerError, nil))
			return
		}
	}
	marshalCacheData, err := json.Marshal(session)
	if err != nil {
		h.log.Warn("HandleGetSessionById: failed to marshal session for cache", "session_id", sessionID, "err", err)
	}
	if err := h.redis.Set(c.Request.Context(), cachedKey, marshalCacheData, 5*time.Minute); err != nil {
		h.log.Warn("HandleGetSessionById: failed to save session to cache", "session_id", sessionID, "err", err)
	} else {
		h.log.Info("HandleGetSessionById: session saved to cache", "session_id", sessionID)
	}
	result := ParseResponseWithTrainer(session)
	c.JSON(http.StatusOK, api.NewSuccessResponse("session fetched successfully", api.CodeOK, result, nil))
}

func (h *sessionHandler) StartSessionHandler(c *gin.Context, sessionID uuid.UUID) {
	cachedKey := "session:" + sessionID.String()
	cacheCmd := h.redis.Delete(c.Request.Context(), cachedKey)
	if cacheCmd.Val() == 0 {
		h.log.Info("StartSessionHandler: session not found in cache", "session_id", sessionID)
	}
	if cacheCmd.Err() != nil {
		h.log.Warn("StartSessionHandler: cache delete error", "session_id", sessionID, "err", cacheCmd.Err())
	}
	h.log.Info("StartSessionHandler: cache entry deleted", "session_id", sessionID)
	updateData, err := h.service.StartSession(c.Request.Context(), sessionID)
	if err != nil {
		h.log.Warn("StartSessionHandler: service error", "session_id", sessionID, "err", err)
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, api.NewErrorResponse("failed to get session", api.CodeNotFound, nil))
			return
		} else {
			c.JSON(http.StatusBadRequest, api.NewErrorResponse(err.Error(), api.CodeBadRequest, nil))
			return
		}
	}
	result := ParseResponse(updateData)
	c.JSON(http.StatusOK, api.NewSuccessResponse("session started successfully", api.CodeOK, result, nil))
}

func (h *sessionHandler) JoinSessionHandler(c *gin.Context, sessionID uuid.UUID) {
	cachedKey := "session:" + sessionID.String()
	cacheCmd := h.redis.Delete(c.Request.Context(), cachedKey)
	if cacheCmd.Val() == 0 {
		h.log.Info("JoinSessionHandler: session not found in cache", "session_id", sessionID)
	}
	if cacheCmd.Err() != nil {
		h.log.Warn("JoinSessionHandler: cache delete error", "session_id", sessionID, "err", cacheCmd.Err())
	}
	h.log.Info("JoinSessionHandler: cache entry deleted", "session_id", sessionID)
	updateData, err := h.service.JoinSession(c.Request.Context(), sessionID)
	if err != nil {
		h.log.Warn("JoinSessionHandler: service error", "session_id", sessionID, "err", err)
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, api.NewErrorResponse("failed to get session", api.CodeNotFound, nil))
			return
		} else {
			c.JSON(http.StatusBadRequest, api.NewErrorResponse(err.Error(), api.CodeBadRequest, nil))
			return
		}
	}
	result := ParseResponse(updateData)
	c.JSON(http.StatusOK, api.NewSuccessResponse("session joined successfully", api.CodeOK, result, nil))
}

func (h *sessionHandler) CompleteSession(c *gin.Context, sessionID uuid.UUID) {
	cachedKey := "session:" + sessionID.String()
	cacheCmd := h.redis.Delete(c.Request.Context(), cachedKey)
	if cacheCmd.Val() == 0 {
		h.log.Info("CompleteSession: session not found in cache", "session_id", sessionID)
	}
	if cacheCmd.Err() != nil {
		h.log.Warn("CompleteSession: cache delete error", "session_id", sessionID, "err", cacheCmd.Err())
	}
	h.log.Info("CompleteSession: cache entry deleted", "session_id", sessionID)
	updateData, err := h.service.CompleteSession(c.Request.Context(), sessionID)
	if err != nil {
		h.log.Warn("CompleteSession: service error", "session_id", sessionID, "err", err)
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, api.NewErrorResponse("failed to get session", api.CodeNotFound, nil))
			return
		} else {
			c.JSON(http.StatusBadRequest, api.NewErrorResponse(err.Error(), api.CodeBadRequest, nil))
			return
		}
	}
	if h.notifier != nil || h.mailer != nil {
		bookingID := updateData.BookingID
		go func() {
			notifCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 30*time.Second)
			defer cancel()
			client, err := h.service.GetClientDetailsByBookingID(notifCtx, bookingID)
			if err != nil {
				h.log.Warn("CompleteSession: could not fetch client details for notification", "bookingID", bookingID, "err", err)
				return
			}
			if h.notifier != nil {
				if err := h.notifier.SendNotificationToUser(notifCtx, client.ID,
					"Session Complete",
					"Your session is complete. Please rate your trainer!",
					"session-complete-"+bookingID.String(),
				); err != nil {
					h.log.Warn("CompleteSession: failed to send push notification to client", "err", err)
				}
			}
			if h.mailer != nil {
				if err := h.mailer.SendSessionComplete(client.Email, client.Name, "your trainer"); err != nil {
					h.log.Warn("CompleteSession: failed to send session complete email to client", "err", err)
				}
			}
		}()
	}
	result := ParseResponse(updateData)
	c.JSON(http.StatusOK, api.NewSuccessResponse("session completed successfully", api.CodeOK, result, nil))
}

func (h *sessionHandler) TrainersNote(c *gin.Context, sessionID uuid.UUID) {
	var notes api.HandleTrainersNoteJSONBody
	if err := c.ShouldBindJSON(&notes); err != nil {
		h.log.Warn("error binding request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request, please provide a note", api.CodeBadRequest))
		return
	}
	var fieldErrors []api.FieldError
	if notes.Note == "" {
		h.log.Warn("TrainersNote: note is empty")
		fieldErrors = append(fieldErrors, api.FieldError{Field: "note", Message: "Notes cannot be empty"})
		c.JSON(http.StatusBadRequest, api.NewValidationError(fieldErrors))
		return
	}
	cachedKey := "session:" + sessionID.String()
	cacheCmd := h.redis.Delete(c.Request.Context(), cachedKey)
	if cacheCmd.Val() == 0 {
		h.log.Info("TrainersNote: session not found in cache", "session_id", sessionID)
	}
	if cacheCmd.Err() != nil {
		h.log.Warn("TrainersNote: cache delete error", "session_id", sessionID, "err", cacheCmd.Err())
	}
	h.log.Info("TrainersNote: cache entry deleted", "session_id", sessionID)
	updateData, err := h.service.TrainerSessionNote(c.Request.Context(), sessionID, notes.Note)
	if err != nil {
		h.log.Warn("TrainersNote: service error", "session_id", sessionID, "err", err)
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, api.NewErrorResponse("failed to get session", api.CodeNotFound, nil))
			return
		}
		c.JSON(http.StatusBadRequest, api.NewErrorResponse(err.Error(), api.CodeBadRequest, nil))
		return
	}
	result := ParseResponse(updateData)
	c.JSON(http.StatusOK, api.NewSuccessResponse("trainers note added successfully", api.CodeOK, result, nil))
}
