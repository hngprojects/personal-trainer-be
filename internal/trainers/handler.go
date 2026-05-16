package trainers

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"time"

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
	userID := c.MustGet("user_id").(uuid.UUID)
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

	c.JSON(http.StatusCreated, api.NewSuccess("application submitted", api.CodeCreated, TrainerToMap(trainer)))
}

func (h *Handler) GetTrainerId(c *gin.Context, id uuid.UUID) {
	trainer, err := h.q.GetTrainerByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewError("trainer not found", api.CodeNotFound))
			return
		}
		h.log.Error("get trainer by id failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("could not get trainer", api.CodeServerError))
		return
	}
	c.JSON(http.StatusOK, api.NewSuccess("trainer retrieved", api.CodeOK, TrainerToMap(trainer)))
}

func (h *Handler) GetTrainers(c *gin.Context, params api.GetTrainersParams) {
	dbParams := db.ListTrainersParams{}
	var minRating sql.NullFloat64

	if params.Specialization != nil {
		dbParams.Specialization = sql.NullString{
			String: *params.Specialization,
			Valid:  true,
		}
	}

	if params.MinRating != nil {
		minRating = sql.NullFloat64{
			Float64: *params.MinRating,
			Valid:   true,
		}

		dbParams.MinAverageRating = minRating
	}

	if params.Limit != nil {
		dbParams.Limit = int32(*params.Limit)
	} else {
		dbParams.Limit = 20
	}

	trainers, err := h.q.ListTrainers(c.Request.Context(), dbParams)
	if err != nil {
		h.log.Error("get trainers failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("could not get trainers", api.CodeServerError))
		return
	}

	out := make([]map[string]interface{}, len(trainers))
	for i, t := range trainers {
		out[i] = TrainerToMap(t)
	}

	c.JSON(http.StatusOK, api.NewSuccess("trainers retrieved", api.CodeOK, out))
}

type reviewCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        uuid.UUID `json:"id"`
}

func encodeReviewCursor(r db.Review) string {
	b, _ := json.Marshal(reviewCursor{CreatedAt: r.CreatedAt, ID: r.ID})
	return base64.StdEncoding.EncodeToString(b)
}

func decodeReviewCursor(s string) (reviewCursor, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return reviewCursor{}, err
	}
	var c reviewCursor
	return c, json.Unmarshal(b, &c)
}

func publicReview(r db.Review) map[string]interface{} {
	m := map[string]interface{}{
		"id":         r.ID,
		"trainer_id": r.TrainerID,
		"rating":     r.Rating,
		"created_at": r.CreatedAt,
		"updated_at": r.UpdatedAt,
	}
	if r.Review.Valid {
		m["review"] = r.Review.String
	}
	return m
}

func (h *Handler) GetTrainerReviews(c *gin.Context, id uuid.UUID, params api.GetTrainersIdReviewsParams) {
	ctx := c.Request.Context()
	var limit int32 = 10
	if params.Limit != nil && *params.Limit > 0 {
		limit = int32(*params.Limit)
	}

	var reviews []db.Review
	var err error

	if params.Cursor != nil && *params.Cursor != "" {
		cur, decErr := decodeReviewCursor(*params.Cursor)
		if decErr != nil {
			c.JSON(http.StatusBadRequest, api.NewError("invalid cursor", api.CodeBadRequest))
			return
		}
		reviews, err = h.q.ListTrainerReviewsAfterCursor(ctx, db.ListTrainerReviewsAfterCursorParams{
			TrainerID:       id,
			CursorCreatedAt: cur.CreatedAt,
			CursorID:        cur.ID,
			LimitCount:      limit,
		})
	} else {
		reviews, err = h.q.ListTrainerReviewsFirstPage(ctx, db.ListTrainerReviewsFirstPageParams{
			TrainerID:  id,
			LimitCount: limit,
		})
	}

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewError("trainer not found", api.CodeNotFound))
			return
		}
		h.log.Error("get trainer reviews failed", "err", err, "trainer_id", id)
		c.JSON(http.StatusInternalServerError, api.NewError("could not get trainer reviews", api.CodeServerError))
		return
	}

	public := make([]map[string]interface{}, len(reviews))
	for i, r := range reviews {
		public[i] = publicReview(r)
	}

	hasMore := len(reviews) == int(limit)
	var nextCursor *string
	if hasMore && len(reviews) > 0 {
		s := encodeReviewCursor(reviews[len(reviews)-1])
		nextCursor = &s
	}

	meta := map[string]interface{}{
		"has_more":    hasMore,
		"next_cursor": nextCursor,
	}

	c.JSON(http.StatusOK, api.NewSuccessWithMeta("Reviews retrieved", api.CodeOK, public, meta))
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
