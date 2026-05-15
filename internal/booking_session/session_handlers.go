package booking_session

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/redis"
)

type sessionHandler struct {
	service SessionInterface
	redis   redis.Client
	log     *slog.Logger
}

type SessionHandler interface {
	HandleGetSessionById(c *gin.Context, sessionID uuid.UUID)
	StartSessionHandler(c *gin.Context, sessionID uuid.UUID)
	JoinSessionHandler(c *gin.Context, sessionID uuid.UUID)
	CompleteSession(c *gin.Context, sessionID uuid.UUID)
	TrainersNote(c *gin.Context, sessionID uuid.UUID)
}

func NewSessionHandler(service SessionInterface, redis redis.Client, log *slog.Logger) *sessionHandler {
	return &sessionHandler{service: service, redis: redis, log: log}
}

func (h *sessionHandler) HandleGetSessionById(c *gin.Context, sessionID uuid.UUID) {
	cachedKey := "session:" + sessionID.String()
	cached := h.redis.Get(c.Request.Context(), cachedKey)
	if cached.Err() == nil {
		h.log.Info("Cache hit!!")
		var body db.BookingSession
		if err := json.Unmarshal([]byte(cached.Val()), &body); err != nil {
			h.log.Error("failed to marshal data into body")
		} else {
			result := ParseResponse(&body)
			c.JSON(http.StatusOK, api.NewSuccessResponse("session fetched successfully", api.CodeOK, result, nil))
			return
		}
	} else {
		h.log.Error("redis error", "err", cached.Err())
	}
	session, err := h.service.GetSessionById(c.Request.Context(), sessionID)
	if err != nil {
		h.log.Error("service returned err", "err", err)
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
		h.log.Error("failed to marshall cache data", "err", err)
	}
	if err := h.redis.Set(c.Request.Context(), cachedKey, marshalCacheData, 5*time.Minute); err != nil {
		h.log.Error("failed to save session to cache", "err", err)
	} else {
		h.log.Info("session saved to cache", "info", err)
	}
	result := ParseResponse(session)
	c.JSON(http.StatusOK, api.NewSuccessResponse("session fetched successfully", api.CodeOK, result, nil))
}

func (h *sessionHandler) StartSessionHandler(c *gin.Context, sessionID uuid.UUID) {
	cachedKey := "session:" + sessionID.String()
	cacheCmd := h.redis.Delete(c.Request.Context(), cachedKey)
	if cacheCmd.Val() == 0 {
		h.log.Info("session not found in cache")
	}
	if cacheCmd.Err() != nil {
		h.log.Error("error occured during cache lookup", "err", cacheCmd.Err())
	}
	h.log.Info("data deleted from cache", "data", cacheCmd)
	updateData, err := h.service.StartSession(c.Request.Context(), sessionID)
	if err != nil {
		h.log.Error("service returned err", "err", err)
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
		h.log.Info("session not found in cache")
	}
	if cacheCmd.Err() != nil {
		h.log.Error("error occured during cache lookup", "err", cacheCmd.Err())
	}
	h.log.Info("data deleted from cache", "data", cacheCmd)
	updateData, err := h.service.JoinSession(c.Request.Context(), sessionID)
	if err != nil {
		h.log.Error("service returned err", "err", err)
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
		h.log.Info("session not found in cache")
	}
	if cacheCmd.Err() != nil {
		h.log.Error("error occured during cache lookup", "err", cacheCmd.Err())
	}
	h.log.Info("data deleted from cache", "data", cacheCmd)
	updateData, err := h.service.CompleteSession(c.Request.Context(), sessionID)
	if err != nil {
		h.log.Error("service returned err", "err", err)
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, api.NewErrorResponse("failed to get session", api.CodeNotFound, nil))
			return
		} else {
			c.JSON(http.StatusBadRequest, api.NewErrorResponse(err.Error(), api.CodeBadRequest, nil))
			return
		}
	}
	// send notification to client to rate session.
	result := ParseResponse(updateData)
	c.JSON(http.StatusOK, api.NewSuccessResponse("session completed successfully", api.CodeOK, result, nil))
}

func (h *sessionHandler) TrainersNote(c *gin.Context, sessionID uuid.UUID) {
	var notes api.HandleTrainersNoteJSONBody
	if err := c.ShouldBindJSON(&notes); err != nil {
		h.log.Error("error binding request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError(api.CodeBadRequest, "invalid request, please provide a note"))
		return
	}
	var fieldErrors []api.FieldError
	if notes.Note == "" {
		fieldErrors = append(fieldErrors, api.FieldError{Field: "notes", Message: "Notes cannot be empty"})
		c.JSON(http.StatusBadRequest, api.NewValidationError(fieldErrors))
		return
	}
	cachedKey := "session:" + sessionID.String()
	cacheCmd := h.redis.Delete(c.Request.Context(), cachedKey)
	if cacheCmd.Val() == 0 {
		h.log.Info("session not found in cache")
	}
	if cacheCmd.Err() != nil {
		h.log.Error("error occured during cache lookup", "err", cacheCmd.Err())
	}
	h.log.Info("data deleted from cache", "data", cacheCmd)
	updateData, err := h.service.TrainerSessionNote(c.Request.Context(), sessionID, notes.Note)
	if err != nil {
		h.log.Error("service returned err", "err", err)
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, api.NewErrorResponse("failed to get session", api.CodeNotFound, nil))
			return
		}
		c.JSON(http.StatusNotFound, api.NewErrorResponse(err.Error(), api.CodeBadRequest, nil))
		return
	}
	result := ParseResponse(updateData)
	c.JSON(http.StatusOK, api.NewSuccessResponse("trainers note added successfully", api.CodeOK, result, nil))
}
