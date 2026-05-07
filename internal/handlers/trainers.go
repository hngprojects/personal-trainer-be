package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type TrainersHandler struct {
	q *db.Queries
}

func NewTrainersHandler(q *db.Queries) *TrainersHandler {
	return &TrainersHandler{q: q}
}

type CreateTrainerRequest struct {
	UserID            string  `json:"user_id" binding:"required"`
	Specialization    *string `json:"specialization"`
	Bio               *string `json:"bio"`
	YearsOfExperience *int32  `json:"years_of_experience"`
	IntroVideoURL     *string `json:"intro_video_url"`
	DisplayPicture    *string `json:"display_picture"`
	CalendlyConnected *bool   `json:"calendly_connected"`
	CalendlyLink      *string `json:"calendly_link"`
	OnboardingStatus  *string `json:"onboarding_status"` // pending/approved/rejected/suspended
}

type UpdateTrainerRequest struct {
	Specialization    *string `json:"specialization"`
	Bio               *string `json:"bio"`
	YearsOfExperience *int32  `json:"years_of_experience"`
	IntroVideoURL     *string `json:"intro_video_url"`
	DisplayPicture    *string `json:"display_picture"`
	CalendlyConnected *bool   `json:"calendly_connected"`
	CalendlyLink      *string `json:"calendly_link"`
	OnboardingStatus  *string `json:"onboarding_status"`
}

// GET /trainers?category=...
func (h *TrainersHandler) List(c *gin.Context) {
	category := c.Query("category") // empty means "no filter" (after SQL fix below)

	trainers, err := h.q.ListTrainers(c.Request.Context(), category)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch trainers"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "TRAINERS_FETCHED",
		"data":    trainers,
	})
}

// POST /trainers
func (h *TrainersHandler) Create(c *gin.Context) {
	var req CreateTrainerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "invalid request body"})
		return
	}

	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "invalid user_id"})
		return
	}

	// sqlc generated Column7/Column9 because COALESCE($7, false) and COALESCE($9, 'pending')
	var calendlyConnected any = nil
	if req.CalendlyConnected != nil {
		calendlyConnected = *req.CalendlyConnected
	}

	var onboardingStatus any = nil
	if req.OnboardingStatus != nil {
		onboardingStatus = *req.OnboardingStatus
	}

	trainer, err := h.q.CreateTrainer(c.Request.Context(), db.CreateTrainerParams{
		UserID: userID,

		Specialization: sql.NullString{String: derefString(req.Specialization), Valid: req.Specialization != nil},
		Bio:            sql.NullString{String: derefString(req.Bio), Valid: req.Bio != nil},

		YearsOfExperience: sql.NullInt32{Int32: derefInt32(req.YearsOfExperience), Valid: req.YearsOfExperience != nil},

		IntroVideoUrl:  sql.NullString{String: derefString(req.IntroVideoURL), Valid: req.IntroVideoURL != nil},
		DisplayPicture: sql.NullString{String: derefString(req.DisplayPicture), Valid: req.DisplayPicture != nil},

		Column7:      calendlyConnected,
		CalendlyLink: sql.NullString{String: derefString(req.CalendlyLink), Valid: req.CalendlyLink != nil},
		Column9:      onboardingStatus,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create trainer"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status":  "success",
		"message": "TRAINER_CREATED",
		"data":    trainer,
	})
}

// PUT /trainers/:id (trainer_id)
func (h *TrainersHandler) Update(c *gin.Context) {
	idStr := c.Param("id")
	trainerID, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "invalid id"})
		return
	}

	var req UpdateTrainerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "invalid request body"})
		return
	}

	// Fetch existing trainer so we don't overwrite non-nullable fields when omitted
	existing, err := h.q.GetTrainerByID(c.Request.Context(), trainerID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "trainer not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load trainer"})
		return
	}

	calendlyConnected := existing.CalendlyConnected
	if req.CalendlyConnected != nil {
		calendlyConnected = *req.CalendlyConnected
	}

	onboardingStatus := existing.OnboardingStatus
	if req.OnboardingStatus != nil {
		onboardingStatus = *req.OnboardingStatus
	}

	trainer, err := h.q.UpdateTrainer(c.Request.Context(), db.UpdateTrainerParams{
		ID: trainerID,

		Specialization: sql.NullString{String: derefString(req.Specialization), Valid: req.Specialization != nil},
		Bio:            sql.NullString{String: derefString(req.Bio), Valid: req.Bio != nil},
		YearsOfExperience: sql.NullInt32{
			Int32: derefInt32(req.YearsOfExperience),
			Valid: req.YearsOfExperience != nil,
		},
		IntroVideoUrl:  sql.NullString{String: derefString(req.IntroVideoURL), Valid: req.IntroVideoURL != nil},
		DisplayPicture: sql.NullString{String: derefString(req.DisplayPicture), Valid: req.DisplayPicture != nil},

		CalendlyConnected: calendlyConnected,
		CalendlyLink:      sql.NullString{String: derefString(req.CalendlyLink), Valid: req.CalendlyLink != nil},
		OnboardingStatus:  onboardingStatus,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "trainer not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update trainer"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "TRAINER_UPDATED",
		"data":    trainer,
	})
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt32(i *int32) int32 {
	if i == nil {
		return 0
	}
	return *i
}