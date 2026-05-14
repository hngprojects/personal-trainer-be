package trainers

import (
	"context"
	"database/sql"
	"log/slog"
	"math"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type TrainersStore interface {
	GetUserByID(ctx context.Context, id uuid.UUID) (db.User, error)
}

type Handler struct {
	q     *db.Queries
	users TrainersStore
	log   *slog.Logger
}

func NewHandler(q *db.Queries, users TrainersStore, log *slog.Logger) *Handler {
	return &Handler{q: q, users: users, log: log}
}

// /trainers/apply POST
func (h *Handler) TrainerApply(c *gin.Context) {
	var req struct {
		UserID            string  `json:"user_id" binding:"required"`
		Specialization    *string `json:"specialization"`
		Bio               *string `json:"bio"`
		YearsOfExperience *int    `json:"years_of_experience"`
		IntroVideoUrl     *string `json:"intro_video_url"`
		DisplayPicture    *string `json:"display_picture"`
		CalendlyConnected *bool   `json:"calendly_connected"`
		CalendlyLink      *string `json:"calendly_link"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}
	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid user id", api.CodeBadRequest))
		return
	}
	calConn := false
	if req.CalendlyConnected != nil {
		calConn = *req.CalendlyConnected
	}

	params := db.CreateTrainerParams{
		UserID:            userID,
		Specialization:    nullStringPtr(req.Specialization),
		Bio:               nullStringPtr(req.Bio),
		YearsOfExperience: nullInt32Ptr(req.YearsOfExperience),
		IntroVideoUrl:     nullStringPtr(req.IntroVideoUrl),
		DisplayPicture:    nullStringPtr(req.DisplayPicture),
		CalendlyConnected: calConn,
		CalendlyLink:      nullStringPtr(req.CalendlyLink),
		OnboardingStatus:  "pending",
	}

	trainer, err := h.q.CreateTrainer(c.Request.Context(), params)
	if err != nil {
		h.log.Error("create trainer failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("could not apply as trainer", api.CodeServerError))
		return
	}

	c.JSON(http.StatusCreated, TrainerToMap(trainer))
}

// Helper functions
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

func TrainerToMap(t db.Trainer) map[string]interface{} {
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
	}
	if t.Bio.Valid {
		out["bio"] = t.Bio.String
	}
	if t.YearsOfExperience.Valid {
		out["years_of_experience"] = t.YearsOfExperience.Int32
	}
	if t.IntroVideoUrl.Valid {
		out["intro_video_url"] = t.IntroVideoUrl.String
	}
	if t.DisplayPicture.Valid {
		out["display_picture"] = t.DisplayPicture.String
	}
	if t.CalendlyLink.Valid {
		out["calendly_link"] = t.CalendlyLink.String
	}
	return out
}
