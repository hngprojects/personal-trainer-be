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
	appredis "github.com/hngprojects/personal-trainer-be/pkg/redis"
)

type availabilityStore struct {
	db    *sql.DB
	q     *db.Queries
	redis appredis.Client
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

	trainer, err := s.availability.q.GetTrainerByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer profile"))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load trainer profile", api.CodeServerError))
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

	savedSlots, err := saveAvailabilitySlots(ctx, s.availability, trainer.ID, parsedSlots)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to save availability", api.CodeServerError))
		return
	}

	// Invalidate cached booking slots for this trainer so clients see fresh data.
	s.availability.redis.Delete(ctx, bookingSlotsCacheKey(trainer.ID))

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

// bookingSlotsCacheKey returns the Redis key for a trainer's cached booking slots.
func bookingSlotsCacheKey(trainerID uuid.UUID) string {
	return "booking-slots:trainer:" + trainerID.String()
}

type parsedSlot struct {
	dayOfWeek int16
	startTime time.Time
	endTime   time.Time
	timezone  string
	index     int
}

func validateAndParseSlot(slot api.AvailabilitySlot) (*parsedSlot, error) {
	if slot.DayOfWeek < 0 || slot.DayOfWeek > 6 {
		return nil, fmt.Errorf("day_of_week must be between 0 and 6")
	}

	if _, err := time.LoadLocation(slot.Timezone); err != nil {
		return nil, fmt.Errorf("invalid timezone: must be a valid IANA timezone (e.g. America/New_York)")
	}

	startTime, err := time.Parse("15:04", slot.StartTime)
	if err != nil {
		return nil, fmt.Errorf("invalid start_time format: must be HH:MM (e.g. 09:00)")
	}
	startTime = startTime.In(time.UTC)

	endTime, err := time.Parse("15:04", slot.EndTime)
	if err != nil {
		return nil, fmt.Errorf("invalid end_time format: must be HH:MM (e.g. 17:00)")
	}
	endTime = endTime.In(time.UTC)

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
	byDay := make(map[int16][]*parsedSlot)
	for _, slot := range slots {
		byDay[slot.dayOfWeek] = append(byDay[slot.dayOfWeek], slot)
	}

	for _, daySlots := range byDay {
		if len(daySlots) <= 1 {
			continue
		}

		sort.Slice(daySlots, func(i, j int) bool {
			return daySlots[i].startTime.Before(daySlots[j].startTime)
		})

		for i := 0; i < len(daySlots)-1; i++ {
			current := daySlots[i]
			next := daySlots[i+1]
			if current.endTime.After(next.startTime) {
				return fmt.Errorf("overlapping availability slots on same day")
			}
		}
	}

	return nil
}

func saveAvailabilitySlots(ctx context.Context, store *availabilityStore, trainerID uuid.UUID, slots []*parsedSlot) ([]db.TrainerAvailability, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	qtx := store.q.WithTx(tx)

	if err := qtx.DeleteTrainerAvailabilitySlots(ctx, trainerID); err != nil {
		return nil, err
	}

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

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return savedSlots, nil
}
