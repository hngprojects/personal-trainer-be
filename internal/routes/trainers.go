// routes/trainers.go
package routes

import (
	"database/sql"
	"errors"
	"math"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type trainersStore struct {
	q *db.Queries
}

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

func nullStringPtr(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: *s, Valid: true}
}

func nullInt32Ptr(i *int) sql.NullInt32 {
	if i == nil {
		return sql.NullInt32{Valid: false}
	}
	if *i > math.MaxInt32 || *i < math.MinInt32 {
		return sql.NullInt32{Valid: false}
	}
	return sql.NullInt32{Int32: int32(*i), Valid: true}
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

// POST /trainers
// 201 -> TrainerResponse (data is Trainer)
func (s *routerImpl) CreateTrainer(c *gin.Context) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	var body api.CreateTrainerRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	userID := uuid.UUID(body.UserId)

	calendlyConnected := false
	if body.CalendlyConnected != nil {
		calendlyConnected = *body.CalendlyConnected
	}

	onboardingStatus := "pending"
	if body.OnboardingStatus != nil {
		onboardingStatus = string(*body.OnboardingStatus)
	}

	created, err := s.trainers.q.CreateTrainer(c.Request.Context(), db.CreateTrainerParams{
		UserID:            userID,
		Specialization:    nullStringPtr(body.Specialization),
		Bio:               nullStringPtr(body.Bio),
		YearsOfExperience: nullInt32Ptr(body.YearsOfExperience),
		IntroVideoUrl:     nullStringPtr(body.IntroVideoUrl),
		DisplayPicture:    nullStringPtr(body.DisplayPicture),
		CalendlyConnected: calendlyConnected,
		CalendlyLink:      nullStringPtr(body.CalendlyLink),
		OnboardingStatus:  onboardingStatus,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to create trainer", api.CodeServerError))
		return
	}

	c.JSON(http.StatusCreated, api.NewSuccess("TRAINER_CREATED", api.CodeCreated, trainerToMap(created)))
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

// PATCH /trainers/{id}
// 200 -> TrainerResponse (data is Trainer)
func (s *routerImpl) UpdateTrainer(c *gin.Context, id openapi_types.UUID) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID := uuid.UUID(id)

	var body api.UpdateTrainerRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	existing, err := s.trainers.q.GetTrainerByID(c.Request.Context(), trainerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load trainer", api.CodeServerError))
		return
	}

	calendlyConnected := existing.CalendlyConnected
	if body.CalendlyConnected != nil {
		calendlyConnected = *body.CalendlyConnected
	}

	onboardingStatus := existing.OnboardingStatus
	if body.OnboardingStatus != nil {
		onboardingStatus = string(*body.OnboardingStatus)
	}

	updated, err := s.trainers.q.UpdateTrainer(c.Request.Context(), db.UpdateTrainerParams{
		ID:                trainerID,
		Specialization:    nullStringPtr(body.Specialization),
		Bio:               nullStringPtr(body.Bio),
		YearsOfExperience: nullInt32Ptr(body.YearsOfExperience),
		IntroVideoUrl:     nullStringPtr(body.IntroVideoUrl),
		DisplayPicture:    nullStringPtr(body.DisplayPicture),
		CalendlyConnected: calendlyConnected,
		CalendlyLink:      nullStringPtr(body.CalendlyLink),
		OnboardingStatus:  onboardingStatus,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to update trainer", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("TRAINER_UPDATED", api.CodeOK, trainerToMap(updated)))
}

// DELETE /trainers/{id}
// 204 -> no content
func (s *routerImpl) DeleteTrainer(c *gin.Context, id openapi_types.UUID) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID := uuid.UUID(id)

	_, err := s.trainers.q.DeleteTrainer(c.Request.Context(), trainerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to delete trainer", api.CodeServerError))
		return
	}

	c.Status(http.StatusNoContent)
}