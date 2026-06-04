package routes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	openapi_types "github.com/oapi-codegen/runtime/types"

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

// PutTrainersMeAvailability handles PUT /trainers/me/availability — trainer
// updates their own schedule. Looks up trainer.id via JWT user_id, then
// delegates to the shared replace-availability core.
func (s *routerImpl) PutTrainersMeAvailability(c *gin.Context) {
	if s.availability == nil {
		s.logger.Warn("PutTrainersMeAvailability: availability service is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	userIDVal, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		s.logger.Warn("PutTrainersMeAvailability: missing authenticated user in context")
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		s.logger.Warn("PutTrainersMeAvailability: invalid user id type in context")
		c.JSON(http.StatusUnauthorized, api.NewError("invalid user id", api.CodeUnauthorized))
		return
	}

	trainer, err := s.availability.q.GetTrainerByUserID(c.Request.Context(), userID)
	if err != nil {
		s.logger.Warn("PutTrainersMeAvailability: failed to fetch trainer", "userID", userID, "err", err)
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
// super_admin) sets availability on behalf of a specific trainer.
func (s *routerImpl) PutTrainerAvailability(c *gin.Context, id openapi_types.UUID) {
	if s.availability == nil {
		s.logger.Warn("PutTrainerAvailability: availability service is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID := uuid.UUID(id)
	if _, err := s.availability.q.GetTrainerByID(c.Request.Context(), trainerID); err != nil {
		s.logger.Warn("failed to fetch trainer", "trainerID", trainerID, "err", err)
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
// then runs the delete-then-insert in a single TX.
func (s *routerImpl) replaceTrainerAvailability(c *gin.Context, trainerID uuid.UUID) {
	var req api.SetAvailabilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.Warn("replaceTrainerAvailability: invalid request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	if req.Availability == nil {
		s.logger.Warn("replaceTrainerAvailability: nil availability in request")
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "availability", Message: "availability is required (use [] to clear the schedule)"},
		}))
		return
	}

	parsedSlots := make([]*parsedSlot, 0, len(req.Availability))
	for i, slot := range req.Availability {
		parsed, validationErr := validateAndParseSlot(slot)
		if validationErr != nil {
			s.logger.Warn("replaceTrainerAvailability: slot validation failed", "index", i, "err", validationErr)
			c.JSON(http.StatusBadRequest, api.NewError(validationErr.Error(), api.CodeBadRequest))
			return
		}
		parsed.index = i
		parsedSlots = append(parsedSlots, parsed)
	}

	if err := checkOverlaps(parsedSlots); err != nil {
		s.logger.Warn("replaceTrainerAvailability: overlapping slots", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
		return
	}

	savedSlots, err := saveAvailabilitySlots(c.Request.Context(), s.availability, trainerID, parsedSlots)
	if err != nil {
		s.logger.Warn("failed to save trainer availability", "trainerID", trainerID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to save availability", api.CodeServerError))
		return
	}

	if err := s.availability.redis.Delete(c.Request.Context(), bookingSlotsCacheKey(trainerID)); err != nil {
		slog.Warn("failed to invalidate booking slots cache", "trainer_id", trainerID, "err", err)
	}

	c.JSON(http.StatusOK, api.NewSuccess("AVAILABILITY_SET", api.CodeOK, availabilitySlotsToResponse(savedSlots)))
}

// GetTrainersMeAvailability handles GET /trainers/me/availability —
// the calling trainer fetches their own schedule.
func (s *routerImpl) GetTrainersMeAvailability(c *gin.Context) {
	if s.availability == nil {
		s.logger.Warn("GetTrainersMeAvailability: availability service is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	userIDVal, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		s.logger.Warn("GetTrainersMeAvailability: missing authenticated user in context")
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		s.logger.Warn("GetTrainersMeAvailability: invalid user id type in context")
		c.JSON(http.StatusUnauthorized, api.NewError("invalid user id", api.CodeUnauthorized))
		return
	}

	trainer, err := s.availability.q.GetTrainerByUserID(c.Request.Context(), userID)
	if err != nil {
		s.logger.Warn("GetTrainersMeAvailability: failed to fetch trainer", "userID", userID, "err", err)
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
// authenticated user reads a trainer's weekly slots.
func (s *routerImpl) GetTrainerAvailability(c *gin.Context, id openapi_types.UUID) {
	if s.availability == nil {
		s.logger.Warn("GetTrainerAvailability: availability service is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID := uuid.UUID(id)
	if _, err := s.availability.q.GetTrainerByID(c.Request.Context(), trainerID); err != nil {
		s.logger.Warn("failed to fetch trainer", "trainerID", trainerID, "err", err)
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to look up trainer", api.CodeServerError))
		return
	}

	s.fetchTrainerAvailability(c, trainerID)
}

func (s *routerImpl) fetchTrainerAvailability(c *gin.Context, trainerID uuid.UUID) {
	slots, err := s.availability.q.GetTrainerAvailabilitySlots(c.Request.Context(), trainerID)
	if err != nil {
		s.logger.Warn("failed to fetch trainer availability", "trainerID", trainerID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load availability", api.CodeServerError))
		return
	}
	c.JSON(http.StatusOK, api.NewSuccess("AVAILABILITY_FETCHED", api.CodeOK, availabilitySlotsToResponse(slots)))
}

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

// AddTrainersMeAvailability handles POST /trainers/me/availability —
// additive variant of PUT. Looks up trainer.id via JWT user_id, then
// hands off to the shared core that appends without deleting.
func (s *routerImpl) AddTrainersMeAvailability(c *gin.Context) {
	if s.availability == nil {
		s.logger.Warn("AddTrainersMeAvailability: availability service is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	userIDVal, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		s.logger.Warn("AddTrainersMeAvailability: missing authenticated user")
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		s.logger.Warn("AddTrainersMeAvailability: invalid user id type")
		c.JSON(http.StatusUnauthorized, api.NewError("invalid user id", api.CodeUnauthorized))
		return
	}

	trainer, err := s.availability.q.GetTrainerByUserID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer profile"))
			return
		}
		s.logger.Warn("AddTrainersMeAvailability: failed to fetch trainer", "userID", userID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load trainer profile", api.CodeServerError))
		return
	}

	s.appendTrainerAvailability(c, trainer.ID)
}

// AddTrainerAvailability handles POST /trainers/{id}/availability —
// admin appends slots to a specific trainer without wiping existing ones.
func (s *routerImpl) AddTrainerAvailability(c *gin.Context, id openapi_types.UUID) {
	if s.availability == nil {
		s.logger.Warn("AddTrainerAvailability: availability service is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID := uuid.UUID(id)
	if _, err := s.availability.q.GetTrainerByID(c.Request.Context(), trainerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		s.logger.Warn("AddTrainerAvailability: failed to fetch trainer", "trainerID", trainerID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to look up trainer", api.CodeServerError))
		return
	}

	s.appendTrainerAvailability(c, trainerID)
}

// appendTrainerAvailability is the shared body for both POST handlers
// (me + admin). Parses + validates the request, then hands off to a
// single-TX append-with-lock so the read-of-existing-slots and the
// inserts are serialized per-trainer. Exact-duplicate of an existing
// (day, start, end) tuple returns 409; any other overlap returns 400.
func (s *routerImpl) appendTrainerAvailability(c *gin.Context, trainerID uuid.UUID) {
	var req api.AddAvailabilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.Warn("appendTrainerAvailability: invalid request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	if len(req.Availability) == 0 {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "availability", Message: "at least one availability slot is required (use PUT to clear the schedule)"},
		}))
		return
	}

	parsedSlots := make([]*parsedSlot, 0, len(req.Availability))
	for i, slot := range req.Availability {
		parsed, validationErr := validateAndParseSlot(slot)
		if validationErr != nil {
			s.logger.Warn("appendTrainerAvailability: slot validation failed", "index", i, "err", validationErr)
			c.JSON(http.StatusBadRequest, api.NewError(validationErr.Error(), api.CodeBadRequest))
			return
		}
		parsed.index = i
		parsedSlots = append(parsedSlots, parsed)
	}

	// In-request checks first — these don't need DB. Same priority as
	// the cross-existing case below: duplicate wins over overlap so the
	// caller gets a precise 409 instead of a generic 400.
	if err := checkInRequestDuplicate(parsedSlots); err != nil {
		c.JSON(http.StatusConflict, api.NewError(err.Error(), api.CodeConflict))
		return
	}
	if err := checkOverlaps(parsedSlots); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
		return
	}

	saved, err := appendAvailabilitySlots(c.Request.Context(), s.availability, trainerID, parsedSlots)
	if err != nil {
		if errors.Is(err, errAvailabilitySlotDuplicate) {
			c.JSON(http.StatusConflict, api.NewError("one of the supplied slots already exists for this trainer", api.CodeConflict))
			return
		}
		if errors.Is(err, errAvailabilitySlotOverlap) {
			c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
			return
		}
		s.logger.Warn("appendTrainerAvailability: insert failed", "trainerID", trainerID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to save availability", api.CodeServerError))
		return
	}

	if err := s.availability.redis.Delete(c.Request.Context(), bookingSlotsCacheKey(trainerID)); err != nil {
		s.logger.Warn("appendTrainerAvailability: cache invalidate failed", "trainer_id", trainerID, "err", err)
	}

	c.JSON(http.StatusCreated, api.NewSuccess("AVAILABILITY_ADDED", api.CodeCreated, availabilitySlotsToResponse(saved)))
}

// DeleteTrainersMeAvailabilitySlot handles DELETE /trainers/me/availability/{slot_id}.
func (s *routerImpl) DeleteTrainersMeAvailabilitySlot(c *gin.Context, slotID openapi_types.UUID) {
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
		s.logger.Warn("DeleteTrainersMeAvailabilitySlot: trainer lookup failed", "userID", userID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load trainer profile", api.CodeServerError))
		return
	}

	s.deleteOneAvailabilitySlot(c, trainer.ID, uuid.UUID(slotID))
}

// DeleteTrainerAvailabilitySlot handles
// DELETE /trainers/{id}/availability/{slot_id} — admin-only single-slot remove.
func (s *routerImpl) DeleteTrainerAvailabilitySlot(c *gin.Context, id openapi_types.UUID, slotID openapi_types.UUID) {
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
		s.logger.Warn("DeleteTrainerAvailabilitySlot: trainer lookup failed", "trainerID", trainerID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to look up trainer", api.CodeServerError))
		return
	}

	s.deleteOneAvailabilitySlot(c, trainerID, uuid.UUID(slotID))
}

func (s *routerImpl) deleteOneAvailabilitySlot(c *gin.Context, trainerID, slotID uuid.UUID) {
	rows, err := s.availability.q.DeleteAvailabilitySlotByID(c.Request.Context(), db.DeleteAvailabilitySlotByIDParams{
		ID:        slotID,
		TrainerID: trainerID,
	})
	if err != nil {
		s.logger.Warn("deleteOneAvailabilitySlot: delete failed", "trainerID", trainerID, "slotID", slotID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to delete availability slot", api.CodeServerError))
		return
	}
	if rows == 0 {
		// Either the slot doesn't exist OR it belongs to another trainer.
		// Don't distinguish — same 404 keeps the data-layer authz from
		// leaking through the response.
		c.JSON(http.StatusNotFound, api.NewNotFoundError("availability slot"))
		return
	}

	if err := s.availability.redis.Delete(c.Request.Context(), bookingSlotsCacheKey(trainerID)); err != nil {
		s.logger.Warn("deleteOneAvailabilitySlot: cache invalidate failed", "trainer_id", trainerID, "err", err)
	}

	c.Status(http.StatusNoContent)
}

// Sentinels appendAvailabilitySlots returns for caller-mapped responses.
//   - duplicate → 409 (exact same (day, start, end) as an existing row)
//   - overlap   → 400 (partial collision, ranges intersect but not equal)
// Any other error bubbles up untyped and surfaces as 500.
var (
	errAvailabilitySlotDuplicate = errors.New("availability slot duplicates an existing row")
	errAvailabilitySlotOverlap   = errors.New("new availability slot overlaps an existing slot on the same day")
)

// appendAvailabilitySlots runs the whole "read existing → overlap-check
// → insert" sequence inside one TX, with a per-trainer advisory lock
// taken first so concurrent POSTs for the same trainer serialize
// instead of racing. Closes the TOCTOU window the unlocked version
// had: two requests could both pass an overlap check against an empty
// schedule and then commit overlapping rows.
//
// The lock is transaction-scoped (pg_advisory_xact_lock), released
// automatically on COMMIT or ROLLBACK. Different trainers don't block
// each other because the lock key is derived from trainer_id.
func appendAvailabilitySlots(ctx context.Context, store *availabilityStore, trainerID uuid.UUID, slots []*parsedSlot) ([]db.TrainerAvailability, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	qtx := store.q.WithTx(tx)

	// Serialize concurrent appends for this trainer. Blocks (does not
	// error) if another TX holds the lock; released at COMMIT/ROLLBACK.
	if err := qtx.LockTrainerAvailability(ctx, trainerID.String()); err != nil {
		return nil, fmt.Errorf("acquire trainer availability lock: %w", err)
	}

	// Read existing AFTER the lock so the snapshot is consistent for
	// the duration of the TX — concurrent appends are now queued behind us.
	existing, err := qtx.GetTrainerAvailabilitySlots(ctx, trainerID)
	if err != nil {
		return nil, fmt.Errorf("load existing slots: %w", err)
	}

	if err := classifyAgainstExisting(slots, existing); err != nil {
		return nil, err
	}

	saved := make([]db.TrainerAvailability, 0, len(slots))
	for _, slot := range slots {
		row, err := qtx.InsertAvailabilitySlot(ctx, db.InsertAvailabilitySlotParams{
			TrainerID: trainerID,
			DayOfWeek: slot.dayOfWeek,
			StartTime: slot.startTime,
			EndTime:   slot.endTime,
			Timezone:  slot.timezone,
		})
		if err != nil {
			// Belt-and-braces: the duplicate/overlap pre-check above
			// should catch everything, but if the UNIQUE constraint
			// fires anyway (e.g. a new code path inserts without
			// going through here), map cleanly to 409 rather than 500.
			if isUniqueViolation(err) {
				return nil, errAvailabilitySlotDuplicate
			}
			return nil, err
		}
		saved = append(saved, row)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return saved, nil
}

// isUniqueViolation reports whether err is a pq UNIQUE constraint
// violation (Postgres SQLSTATE 23505). Kept narrow so we don't
// accidentally swallow other constraint errors.
func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

// classifyAgainstExisting walks each new slot against the persisted set
// and returns:
//   - errAvailabilitySlotDuplicate if any new slot shares (day, start, end)
//     with an existing row (UNIQUE-constraint shape; maps to 409)
//   - errAvailabilitySlotOverlap otherwise, if a time range intersects
//     a non-equal existing row on the same day (maps to 400)
//   - nil if every new slot is independent of every existing one
//
// Duplicate is checked first so the caller gets a precise 409 — the
// pure-overlap predicate (new.start < existing.end AND new.end >
// existing.start) is true for exact equality too, so without this
// ordering exact dupes would always look like generic overlaps.
func classifyAgainstExisting(newSlots []*parsedSlot, existing []db.TrainerAvailability) error {
	for _, n := range newSlots {
		for _, e := range existing {
			if e.DayOfWeek != n.dayOfWeek {
				continue
			}
			if n.startTime.Equal(e.StartTime) && n.endTime.Equal(e.EndTime) {
				return errAvailabilitySlotDuplicate
			}
			if n.startTime.Before(e.EndTime) && n.endTime.After(e.StartTime) {
				return errAvailabilitySlotOverlap
			}
		}
	}
	return nil
}

// checkInRequestDuplicate rejects two slots in the same request body
// that share (day, start, end). The general in-request overlap check
// (checkOverlaps) would also fire on these, but as a generic 400; this
// one returns a typed error so the caller can surface 409 instead.
func checkInRequestDuplicate(slots []*parsedSlot) error {
	type key struct {
		day   int16
		start time.Time
		end   time.Time
	}
	seen := make(map[key]struct{}, len(slots))
	for _, s := range slots {
		k := key{day: s.dayOfWeek, start: s.startTime, end: s.endTime}
		if _, dup := seen[k]; dup {
			return fmt.Errorf("two slots in the request share the same (day_of_week=%d, start, end)", s.dayOfWeek)
		}
		seen[k] = struct{}{}
	}
	return nil
}
