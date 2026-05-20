package routes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type availabilityStore struct {
	db *sql.DB
	q  *db.Queries
}

// PutTrainersMeAvailability handles PUT /trainers/me/availability — trainer
// updates their own schedule. Looks up trainer.id via JWT user_id, then
// delegates to the shared replace-availability core.
func (s *routerImpl) PutTrainersMeAvailability(c *gin.Context) {
	if s.availability == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	userIDVal, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("invalid user id", api.CodeUnauthorized))
		return
	}

	trainer, err := s.availability.q.GetTrainerByUserID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer profile"))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load trainer profile", api.CodeServerError))
		return
	}

	s.replaceTrainerAvailability(c, trainer.ID)
}

// PutTrainerAvailability handles PUT /trainers/{id}/availability — admin (or
// super_admin) sets availability on behalf of a specific trainer. The
// TrainersAdminOnly middleware gates non-GET /trainers/{id}/* routes to
// admin roles, so we don't need a manual role check here. We still verify
// the trainer row exists so a typo in the URL returns 404 rather than
// silently creating availability rows pointing at a non-existent trainer
// (which would also fail the FK, but the 404 is clearer).
func (s *routerImpl) PutTrainerAvailability(c *gin.Context, id openapi_types.UUID) {
	if s.availability == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID := uuid.UUID(id)
	if _, err := s.availability.q.GetTrainerByID(c.Request.Context(), trainerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to look up trainer", api.CodeServerError))
		return
	}

	s.replaceTrainerAvailability(c, trainerID)
}

// replaceTrainerAvailability is the shared body of the two PUT handlers
// (me + admin). Parses the request, validates each slot, rejects overlaps,
// then runs the delete-then-insert in a single TX. Lives as a method on
// routerImpl so we don't have to thread `s.availability` through as an
// extra arg.
func (s *routerImpl) replaceTrainerAvailability(c *gin.Context, trainerID uuid.UUID) {
	var req api.SetAvailabilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	// The OpenAPI schema marks `availability` required, but gin's JSON
	// binder doesn't enforce that — a missing field or an explicit `null`
	// both decode to a nil slice, indistinguishable from one another in
	// Go. We MUST distinguish "field absent" (mistake — reject) from
	// "field is []" (deliberate — clear the schedule). Since encoding/json
	// produces a non-nil empty slice for `[]` and a nil slice for both
	// missing-field and `null`, gating on `== nil` handles both
	// reject-able cases. Without this check a payload like `{}` silently
	// wipes the trainer's schedule.
	if req.Availability == nil {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "availability", Message: "availability is required (use [] to clear the schedule)"},
		}))
		return
	}

	parsedSlots := make([]*parsedSlot, 0, len(req.Availability))
	for i, slot := range req.Availability {
		parsed, validationErr := validateAndParseSlot(slot)
		if validationErr != nil {
			c.JSON(http.StatusBadRequest, api.NewError(validationErr.Error(), api.CodeBadRequest))
			return
		}
		parsed.index = i
		parsedSlots = append(parsedSlots, parsed)
	}

	if err := checkOverlaps(parsedSlots); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
		return
	}

	savedSlots, err := saveAvailabilitySlots(c.Request.Context(), s.availability, trainerID, parsedSlots)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to save availability", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("AVAILABILITY_SET", api.CodeOK, availabilitySlotsToResponse(savedSlots)))
}

// GetTrainersMeAvailability handles GET /trainers/me/availability —
// the calling trainer fetches their own schedule. Useful for the dashboard
// to populate the editor before the trainer makes changes.
func (s *routerImpl) GetTrainersMeAvailability(c *gin.Context) {
	if s.availability == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	userIDVal, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("invalid user id", api.CodeUnauthorized))
		return
	}

	trainer, err := s.availability.q.GetTrainerByUserID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer profile"))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load trainer profile", api.CodeServerError))
		return
	}

	s.fetchTrainerAvailability(c, trainer.ID)
}

// GetTrainerAvailability handles GET /trainers/{id}/availability — any
// authenticated user (clients shopping for a trainer, admins inspecting
// the schedule, the trainer themselves via their public id) reads a
// trainer's weekly slots. Gated only by the global bearer-auth middleware
// — TrainersAdminOnly admits all GETs on /trainers/{id}/*.
func (s *routerImpl) GetTrainerAvailability(c *gin.Context, id openapi_types.UUID) {
	if s.availability == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID := uuid.UUID(id)
	if _, err := s.availability.q.GetTrainerByID(c.Request.Context(), trainerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to look up trainer", api.CodeServerError))
		return
	}

	s.fetchTrainerAvailability(c, trainerID)
}

// fetchTrainerAvailability is the shared body of the two GET handlers (me
// + by-id). Returns an empty array (not null) when the trainer has no
// slots saved yet — matches the SetAvailabilityResponse shape.
func (s *routerImpl) fetchTrainerAvailability(c *gin.Context, trainerID uuid.UUID) {
	slots, err := s.availability.q.GetTrainerAvailabilitySlots(c.Request.Context(), trainerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load availability", api.CodeServerError))
		return
	}
	c.JSON(http.StatusOK, api.NewSuccess("AVAILABILITY_FETCHED", api.CodeOK, availabilitySlotsToResponse(slots)))
}

// availabilitySlotsToResponse renders DB rows in the API's
// AvailabilitySlot shape. Always returns a non-nil slice so JSON encodes
// as `[]` rather than `null` for trainers with no schedule.
func availabilitySlotsToResponse(slots []db.TrainerAvailability) []api.AvailabilitySlot {
	out := make([]api.AvailabilitySlot, len(slots))
	for i, slot := range slots {
		out[i] = api.AvailabilitySlot{
			DayOfWeek: int(slot.DayOfWeek),
			StartTime: slot.StartTime.Format("15:04"),
			EndTime:   slot.EndTime.Format("15:04"),
			Timezone:  slot.Timezone,
		}
	}
	return out
}

type parsedSlot struct {
	dayOfWeek int16
	startTime time.Time
	endTime   time.Time
	timezone  string
	index     int
}

func validateAndParseSlot(slot api.AvailabilitySlot) (*parsedSlot, error) {
	// Validate day_of_week range
	if slot.DayOfWeek < 0 || slot.DayOfWeek > 6 {
		return nil, fmt.Errorf("day_of_week must be between 0 and 6")
	}

	// Validate timezone
	if _, err := time.LoadLocation(slot.Timezone); err != nil {
		return nil, fmt.Errorf("invalid timezone: must be a valid IANA timezone (e.g. America/New_York)")
	}

	// Parse start time in UTC for consistency across all users
	startTime, err := time.Parse("15:04", slot.StartTime)
	if err != nil {
		return nil, fmt.Errorf("invalid start_time format: must be HH:MM (e.g. 09:00)")
	}
	startTime = startTime.In(time.UTC)

	// Parse end time in UTC for consistency across all users
	endTime, err := time.Parse("15:04", slot.EndTime)
	if err != nil {
		return nil, fmt.Errorf("invalid end_time format: must be HH:MM (e.g. 17:00)")
	}
	endTime = endTime.In(time.UTC)

	// Validate end > start
	if !endTime.After(startTime) {
		return nil, fmt.Errorf("end_time must be after start_time")
	}

	return &parsedSlot{
		dayOfWeek: int16(slot.DayOfWeek),
		startTime: startTime,
		endTime:   endTime,
		timezone:  slot.Timezone,
	}, nil
}

func checkOverlaps(slots []*parsedSlot) error {
	// Group by day_of_week
	byDay := make(map[int16][]*parsedSlot)
	for _, slot := range slots {
		byDay[slot.dayOfWeek] = append(byDay[slot.dayOfWeek], slot)
	}

	// For each day, check for overlaps
	for _, daySlots := range byDay {
		if len(daySlots) <= 1 {
			continue
		}

		// Sort by start time
		sort.Slice(daySlots, func(i, j int) bool {
			return daySlots[i].startTime.Before(daySlots[j].startTime)
		})

		// Check consecutive pairs for overlap
		for i := 0; i < len(daySlots)-1; i++ {
			current := daySlots[i]
			next := daySlots[i+1]

			// Overlap if current.end > next.start
			if current.endTime.After(next.startTime) {
				return fmt.Errorf("overlapping availability slots on same day")
			}
		}
	}

	return nil
}

func saveAvailabilitySlots(ctx context.Context, store *availabilityStore, trainerID uuid.UUID, slots []*parsedSlot) ([]db.TrainerAvailability, error) {
	// Start transaction
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	qtx := store.q.WithTx(tx)

	// Delete existing slots
	if err := qtx.DeleteTrainerAvailabilitySlots(ctx, trainerID); err != nil {
		return nil, err
	}

	// Insert new slots
	var savedSlots []db.TrainerAvailability
	for _, slot := range slots {
		saved, err := qtx.InsertAvailabilitySlot(ctx, db.InsertAvailabilitySlotParams{
			TrainerID: trainerID,
			DayOfWeek: slot.dayOfWeek,
			StartTime: slot.startTime,
			EndTime:   slot.endTime,
			Timezone:  slot.timezone,
		})
		if err != nil {
			return nil, err
		}
		savedSlots = append(savedSlots, saved)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return savedSlots, nil
}
