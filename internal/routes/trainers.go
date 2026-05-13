package routes

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/api/validators"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type trainersStore struct {
	q *db.Queries
}

type TrainerLoginRequest struct {
	UserID   string `json:"user_id"`
	Password string `json:"password"`
}

type TrainerLoginData struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type TrainerSetupPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type TrainerSetupPasswordData struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type PutMyAvailabilityRequest struct {
	Timezone string             `json:"timezone"`
	Slots    []AvailabilitySlot `json:"slots"`
}

type AvailabilitySlot struct {
	DayOfWeek int    `json:"day_of_week"` // 0=Sun..6=Sat
	StartTime string `json:"start_time"`  // "HH:MM"
	EndTime   string `json:"end_time"`    // "HH:MM"
}

type AvailabilityWindow struct {
	Start string `json:"start"` // RFC3339 in client tz
	End   string `json:"end"`   // RFC3339 in client tz
}

const (
	maxFailedAttempts = 5
	lockoutDuration   = 15 * time.Minute
)

func newTrainersStore(q *db.Queries) *trainersStore { return &trainersStore{q: q} }

func trainerToMap(t db.Trainer) map[string]interface{} {
	out := map[string]interface{}{
		"id":                 t.ID.String(),
		"user_id":            t.UserID.String(),
		"calendly_connected": t.CalendlyConnected,
		"onboarding_status":  t.OnboardingStatus,
		"average_rating":     t.AverageRating,
		"total_reviews":      t.TotalReviews,
		"created_at":         t.CreatedAt,
		"updated_at":         t.UpdatedAt,
	}

	if t.Specialization.Valid {
		out["specialization"] = t.Specialization.String
	} else {
		out["specialization"] = nil
	}
	if t.Bio.Valid {
		out["bio"] = t.Bio.String
	} else {
		out["bio"] = nil
	}
	if t.YearsOfExperience.Valid {
		out["years_of_experience"] = t.YearsOfExperience.Int32
	} else {
		out["years_of_experience"] = nil
	}
	if t.IntroVideoUrl.Valid {
		out["intro_video_url"] = t.IntroVideoUrl.String
	} else {
		out["intro_video_url"] = nil
	}
	if t.DisplayPicture.Valid {
		out["display_picture"] = t.DisplayPicture.String
	} else {
		out["display_picture"] = nil
	}
	if t.CalendlyLink.Valid {
		out["calendly_link"] = t.CalendlyLink.String
	} else {
		out["calendly_link"] = nil
	}

	return out
}

// GET /trainers?category=...
// 200 -> TrainersListResponse (data is []Trainer)
func (s *routerImpl) GetTrainers(c *gin.Context, params api.GetTrainersParams) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	category := ""
	if params.Category != nil {
		category = *params.Category
	}

	trainers, err := s.trainers.q.ListTrainers(c.Request.Context(), category)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to get trainers", api.CodeServerError))
		return
	}

	list := make([]interface{}, 0, len(trainers))
	for _, t := range trainers {
		list = append(list, trainerToMap(t))
	}

	c.JSON(http.StatusOK, api.NewSuccess("TRAINERS_FETCHED", api.CodeOK, list))
}

// GET /trainers/{id}
// 200 -> TrainerResponse (data is Trainer)
func (s *routerImpl) GetTrainerByID(c *gin.Context, id openapi_types.UUID) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID := uuid.UUID(id)

	t, err := s.trainers.q.GetTrainerByID(c.Request.Context(), trainerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to get trainer", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("TRAINER_FETCHED", api.CodeOK, trainerToMap(t)))
}

// POST /trainers/login
// 200 -> { access_token, refresh_token }
func (s *routerImpl) TrainerLogin(c *gin.Context) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	var req TrainerLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	uid, err := uuid.Parse(req.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "user_id", Message: "must be a valid UUID"},
		}))
		return
	}
	if req.Password == "" {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "password", Message: "password is required"},
		}))
		return
	}

	ctx := c.Request.Context()

	// Ensure login_security row exists (so later updates never hit sql.ErrNoRows)
	if _, err := s.trainers.q.UpsertLoginSecurityRow(ctx, uid); err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	sec, err := s.trainers.q.GetLoginSecurityByUserID(ctx, uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	// Enforce lockout (423)
	if sec.LockedUntil.Valid && sec.LockedUntil.Time.After(time.Now()) {
		remaining := time.Until(sec.LockedUntil.Time).Round(time.Second).String()
		// If you don't have a "locked" code constant, reuse bad request/forbidden consistently.
		c.JSON(http.StatusLocked, api.NewError("account locked, try again in "+remaining, api.CodeForbidden))
		return
	}

	user, err := s.trainers.q.GetUserByID(ctx, uid)
	// Avoid user enumeration: treat not-found as invalid creds, still count failure
	if err != nil {
		_, _, locked := incrementAndMaybeLock(ctx, s.trainers.q, uid)
		if locked {
			sec2, _ := s.trainers.q.GetLoginSecurityByUserID(ctx, uid)
			remaining := time.Until(sec2.LockedUntil.Time).Round(time.Second).String()
			c.JSON(http.StatusLocked, api.NewError("account locked, try again in "+remaining, api.CodeForbidden))
			return
		}
		c.JSON(http.StatusUnauthorized, api.NewError("invalid credentials", api.CodeUnauthorized))
		return
	}

	// Role check via roles tables
	hasTrainerRole, err := s.trainers.q.UserHasRole(ctx, db.UserHasRoleParams{
		UserID: user.ID,
		Name:   "trainer",
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	if !hasTrainerRole {
		c.JSON(http.StatusForbidden, api.NewError("forbidden", api.CodeForbidden))
		return
	}

	// Active/suspended check
	if !user.IsActive {
		c.JSON(http.StatusForbidden, api.NewError("account is suspended", api.CodeForbidden))
		return
	}

	// Ensure user has a trainer profile (should always be true for valid users, but just in case)
	if _, err := s.trainers.q.GetTrainerByUserID(ctx, user.ID); err != nil {
		c.JSON(http.StatusForbidden, api.NewError("forbidden", api.CodeForbidden))
		return
	}

	// Password must be set
	if !user.Password.Valid || user.Password.String == "" {
		_, _, locked := incrementAndMaybeLock(ctx, s.trainers.q, uid)
		if locked {
			sec2, _ := s.trainers.q.GetLoginSecurityByUserID(ctx, uid)
			remaining := time.Until(sec2.LockedUntil.Time).Round(time.Second).String()
			c.JSON(http.StatusLocked, api.NewError("account locked, try again in "+remaining, api.CodeForbidden))
			return
		}
		c.JSON(http.StatusUnauthorized, api.NewError("invalid credentials", api.CodeUnauthorized))
		return
	}

	// Verify password
	if err := auth.CheckPassword(user.Password.String, req.Password); err != nil {
		_, _, locked := incrementAndMaybeLock(ctx, s.trainers.q, uid)
		if locked {
			sec2, _ := s.trainers.q.GetLoginSecurityByUserID(ctx, uid)
			remaining := time.Until(sec2.LockedUntil.Time).Round(time.Second).String()
			c.JSON(http.StatusLocked, api.NewError("account locked, try again in "+remaining, api.CodeForbidden))
			return
		}
		c.JSON(http.StatusUnauthorized, api.NewError("invalid credentials", api.CodeUnauthorized))
		return
	}

	// Success: reset counters
	if err := s.trainers.q.ResetLoginSecurityOnSuccess(ctx, uid); err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	access, err := auth.GenerateJWTToken(user.ID.String(), auth.AccessToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to generate token", api.CodeServerError))
		return
	}
	refresh, err := auth.GenerateJWTToken(user.ID.String(), auth.RefreshToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to generate token", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("LOGIN_SUCCESSFUL", api.CodeOK, TrainerLoginData{
		AccessToken:  access,
		RefreshToken: refresh,
	}))
}

// POST /trainers/setup-password
func (s *routerImpl) TrainerSetupPassword(c *gin.Context) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	var req TrainerSetupPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	if req.Token == "" {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "token", Message: "token is required"},
		}))
		return
	}

	if ok, msg := validators.ValidatePasswordStrength(req.Password); !ok {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "password", Message: msg},
		}))
		return
	}

	ctx := c.Request.Context()

	tok, err := s.trainers.q.GetTrainerInviteTokenByToken(ctx, req.Token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusBadRequest, api.NewError("invalid or expired token", api.CodeBadRequest))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	if tok.UsedAt.Valid {
		c.JSON(http.StatusBadRequest, api.NewError("token already used", api.CodeBadRequest))
		return
	}
	if time.Now().After(tok.ExpiresAt) {
		c.JSON(http.StatusBadRequest, api.NewError("token expired", api.CodeBadRequest))
		return
	}

	user, err := s.trainers.q.GetUserByID(ctx, tok.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid token", api.CodeBadRequest))
		return
	}

	// role-check
	hasTrainerRole, err := s.trainers.q.UserHasRole(ctx, db.UserHasRoleParams{
		UserID: user.ID,
		Name:   "trainer",
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	if !hasTrainerRole {
		c.JSON(http.StatusForbidden, api.NewError("forbidden", api.CodeForbidden))
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid password", api.CodeBadRequest))
		return
	}

	// Ideally do these two operations atomically. If you have a transaction helper, use it.
	if err := s.trainers.q.UpdateUserPasswordByID(ctx, db.UpdateUserPasswordByIDParams{
		ID:       tok.UserID,
		Password: sql.NullString{String: hash, Valid: true},
	}); err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to set password", api.CodeServerError))
		return
	}

	if err := s.trainers.q.MarkTrainerInviteTokenUsed(ctx, tok.ID); err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to mark token used", api.CodeServerError))
		return
	}

	access, err := auth.GenerateJWTToken(user.ID.String(), auth.AccessToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to generate token", api.CodeServerError))
		return
	}
	refresh, err := auth.GenerateJWTToken(user.ID.String(), auth.RefreshToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to generate token", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("PASSWORD_SET_SUCCESSFULLY", api.CodeOK, TrainerSetupPasswordData{
		AccessToken:  access,
		RefreshToken: refresh,
	}))
}

func incrementAndMaybeLock(ctx context.Context, q *db.Queries, uid uuid.UUID) (int32, db.LoginSecurity, bool) {
	sec, err := q.IncrementFailedLoginAttempt(ctx, uid)
	if err != nil {
		return 0, db.LoginSecurity{}, false
	}

	if sec.FailedAttempts >= int32(maxFailedAttempts) {
		lockedUntil := time.Now().Add(lockoutDuration)

		sec, err = q.LockUserLoginUntil(ctx, db.LockUserLoginUntilParams{
			UserID:      uid,
			LockedUntil: sql.NullTime{Time: lockedUntil, Valid: true},
		})
		if err != nil {
			return sec.FailedAttempts, sec, false
		}
		return sec.FailedAttempts, sec, true
	}

	return sec.FailedAttempts, sec, false
}

func parseHHMM(s string) (time.Time, error) { return time.Parse("15:04", s) }

// PUT /trainers/me/availability
func (s *routerImpl) PutTrainersMeAvailability(c *gin.Context) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	uidAny, ok := c.Get(common.ContextKeyUserID)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("unauthorized", api.CodeUnauthorized))
		return
	}
	userID := uidAny.(uuid.UUID)

	// Enforce trainer role via user_roles/roles (matches existing auth layer)
	hasTrainerRole, err := s.trainers.q.UserHasRole(c.Request.Context(), db.UserHasRoleParams{
		UserID: userID,
		Name:   "trainer",
	})
	if err != nil || !hasTrainerRole {
		c.JSON(http.StatusForbidden, api.NewError("forbidden", api.CodeForbidden))
		return
	}

	var body PutMyAvailabilityRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	if body.Timezone == "" {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "timezone", Message: "timezone is required"},
		}))
		return
	}
	if _, err := time.LoadLocation(body.Timezone); err != nil {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "timezone", Message: "invalid timezone (expected IANA name like Africa/Lagos)"},
		}))
		return
	}

	// Map user_id -> trainer_id (you must add this query)
	trainer, err := s.trainers.q.GetTrainerByUserID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusForbidden, api.NewError("forbidden", api.CodeForbidden))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load trainer", api.CodeServerError))
		return
	}

	// Validate + overlap checks
	type slotParsed struct {
		day int
		s   time.Time
		e   time.Time
	}
	byDay := map[int][]slotParsed{}

	for idx, sl := range body.Slots {
		if sl.DayOfWeek < 0 || sl.DayOfWeek > 6 {
			c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
				{Field: "slots", Message: "invalid day_of_week at index " + strconv.Itoa(idx)},
			}))
			return
		}
		st, err := parseHHMM(sl.StartTime)
		if err != nil {
			c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
				{Field: "slots", Message: "invalid start_time at index " + strconv.Itoa(idx) + " (expected HH:MM)"},
			}))
			return
		}
		et, err := parseHHMM(sl.EndTime)
		if err != nil {
			c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
				{Field: "slots", Message: "invalid end_time at index " + strconv.Itoa(idx) + " (expected HH:MM)"},
			}))
			return
		}
		if !et.After(st) {
			c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
				{Field: "slots", Message: "end_time must be after start_time at index " + strconv.Itoa(idx)},
			}))
			return
		}
		byDay[sl.DayOfWeek] = append(byDay[sl.DayOfWeek], slotParsed{day: sl.DayOfWeek, s: st, e: et})
	}

	for day, slots := range byDay {
		sort.Slice(slots, func(i, j int) bool { return slots[i].s.Before(slots[j].s) })
		for i := 1; i < len(slots); i++ {
			if slots[i].s.Before(slots[i-1].e) {
				c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
					{Field: "slots", Message: "overlapping slots on day_of_week " + strconv.Itoa(day)},
				}))
				return
			}
		}
	}

	// Persist (delete + insert). Ideally wrap in a transaction later.
	if err := s.trainers.q.DeleteTrainerAvailability(c.Request.Context(), trainer.ID); err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to update availability", api.CodeServerError))
		return
	}

	for _, slots := range byDay {
		for _, sl := range slots {
			_, err := s.trainers.q.CreateTrainerAvailability(c.Request.Context(), db.CreateTrainerAvailabilityParams{
				TrainerID: trainer.ID,
				DayOfWeek: int32(sl.day),
				StartTime: sl.s,
				EndTime:   sl.e,
				Timezone:  body.Timezone,
			})
			if err != nil {
				c.JSON(http.StatusInternalServerError, api.NewError("failed to update availability", api.CodeServerError))
				return
			}
		}
	}

	out, err := s.trainers.q.ListTrainerAvailability(c.Request.Context(), trainer.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to fetch availability", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("AVAILABILITY_UPDATED", api.CodeOK, out))
}

// GET /trainers/:id/availability
func (s *routerImpl) GetTrainersAvailabilityByID(c *gin.Context, id openapi_types.UUID) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	uidAny, ok := c.Get(common.ContextKeyUserID)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("unauthorized", api.CodeUnauthorized))
		return
	}
	userID := uidAny.(uuid.UUID)

	// Enforce client role
	hasClientRole, err := s.trainers.q.UserHasRole(c.Request.Context(), db.UserHasRoleParams{
		UserID: userID,
		Name:   "client",
	})
	if err != nil || !hasClientRole {
		c.JSON(http.StatusForbidden, api.NewError("forbidden", api.CodeForbidden))
		return
	}

	trainerID := uuid.UUID(id)

	// Optional: confirm trainer exists. If you prefer 200+[] even for invalid trainerId, remove this check.
	if _, err := s.trainers.q.GetTrainerByID(c.Request.Context(), trainerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusOK, api.NewSuccess("AVAILABILITY_FETCHED", api.CodeOK, []AvailabilityWindow{}))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load trainer", api.CodeServerError))
		return
	}

	slots, err := s.trainers.q.ListTrainerAvailability(c.Request.Context(), trainerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to fetch availability", api.CodeServerError))
		return
	}
	if len(slots) == 0 {
		c.JSON(http.StatusOK, api.NewSuccess("AVAILABILITY_FETCHED", api.CodeOK, []AvailabilityWindow{}))
		return
	}

	clientTZ := c.GetHeader("X-Client-Timezone")
	clientLoc := time.UTC
	if clientTZ != "" {
		if loc, err := time.LoadLocation(clientTZ); err == nil {
			clientLoc = loc
		}
	}

	now := time.Now()
	windows := make([]AvailabilityWindow, 0, len(slots))

	for _, sl := range slots {
		trainerLoc, err := time.LoadLocation(sl.Timezone)
		if err != nil {
			trainerLoc = time.UTC
		}

		// next date (in trainer tz) matching stored day_of_week
		d := nextWeekdayDate(now.In(trainerLoc), int(sl.DayOfWeek))

		startDT := time.Date(d.Year(), d.Month(), d.Day(),
			sl.StartTime.Hour(), sl.StartTime.Minute(), 0, 0, trainerLoc)
		endDT := time.Date(d.Year(), d.Month(), d.Day(),
			sl.EndTime.Hour(), sl.EndTime.Minute(), 0, 0, trainerLoc)

		windows = append(windows, AvailabilityWindow{
			Start: startDT.In(clientLoc).Format(time.RFC3339),
			End:   endDT.In(clientLoc).Format(time.RFC3339),
		})
	}

	c.JSON(http.StatusOK, api.NewSuccess("AVAILABILITY_FETCHED", api.CodeOK, windows))
}

func nextWeekdayDate(from time.Time, dayOfWeek int) time.Time {
	target := time.Weekday(dayOfWeek) // 0=Sunday
	diff := (int(target) - int(from.Weekday()) + 7) % 7
	return from.AddDate(0, 0, diff)
}
