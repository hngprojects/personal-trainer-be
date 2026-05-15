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

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type availabilityStore struct {
	db *sql.DB
	q  *db.Queries
}

// PutTrainersMeAvailability handles PUT /trainers/me/availability
// Sets trainer weekly availability, replacing all existing slots.
func (s *routerImpl) PutTrainersMeAvailability(c *gin.Context) {
	if s.availability == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	var req api.SetAvailabilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	// Extract authenticated user ID
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

	ctx := c.Request.Context()

	// Lookup trainer by user ID
	trainer, err := s.availability.q.GetTrainerByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer profile"))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load trainer profile", api.CodeServerError))
		return
	}

	// Validate and convert availability slots
	parsedSlots := make([]*parsedSlot, 0, len(req.Availability))
	for i, slot := range req.Availability {
		parsed, validationErr := validateAndParseSlot(slot)
		if validationErr != nil {
			c.JSON(http.StatusBadRequest, api.NewError(validationErr.Error(), api.CodeBadRequest))
			return
		}
		// Add index for error reporting in case of overlaps
		parsed.index = i
		parsedSlots = append(parsedSlots, parsed)
	}

	// Check for overlaps
	if err := checkOverlaps(parsedSlots); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
		return
	}

	// Save slots (delete old, insert new) in transaction
	savedSlots, err := saveAvailabilitySlots(ctx, s.availability, trainer.ID, parsedSlots)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to save availability", api.CodeServerError))
		return
	}

	// Convert saved slots to response format
	responseSlots := make([]api.AvailabilitySlot, len(savedSlots))
	for i, slot := range savedSlots {
		responseSlots[i] = api.AvailabilitySlot{
			DayOfWeek: int(slot.DayOfWeek),
			StartTime: slot.StartTime.Format("15:04"),
			EndTime:   slot.EndTime.Format("15:04"),
			Timezone:  slot.Timezone,
		}
	}

	c.JSON(http.StatusOK, api.NewSuccess("AVAILABILITY_SET", api.CodeOK, responseSlots))
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
	defer tx.Rollback()

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
