package routes

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

func userToProfileMap(u db.User) map[string]interface{} {
	profileComplete :=
		u.Name != "" &&
			u.Gender.Valid &&
			u.FitnessLevel.Valid &&
			u.AvatarUrl.Valid
	out := map[string]interface{}{
		"id":               u.ID.String(),
		"email":            u.Email,
		"name":             u.Name,
		"fitness_goals":    u.FitnessGoals,
		"profile_complete": profileComplete,
	}
	if u.Gender.Valid {
		out["gender"] = u.Gender.String
	} else {
		out["gender"] = nil
	}
	if u.FitnessLevel.Valid {
		out["fitness_level"] = u.FitnessLevel.String
	} else {
		out["fitness_level"] = nil
	}
	if u.AvatarUrl.Valid {
		out["avatar_url"] = u.AvatarUrl.String
	} else {
		out["avatar_url"] = nil
	}
	return out
}

func userIDFromContext(c *gin.Context) (uuid.UUID, bool) {
	val, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		return uuid.Nil, false
	}
	id, ok := val.(uuid.UUID)
	return id, ok
}

// PATCH /users/me/profile
func (s *routerImpl) UpdateUserProfile(c *gin.Context) {
	if s.users == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}

	var body api.UpdateProfileRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	if body.Gender != nil && !body.Gender.Valid() {
		c.JSON(http.StatusBadRequest, api.NewError("gender must be male, female, or other", api.CodeBadRequest))
		return
	}

	if body.FitnessLevel != nil && !body.FitnessLevel.Valid() {
		c.JSON(http.StatusBadRequest, api.NewError("fitness_level must be beginner, intermediate, or advanced", api.CodeBadRequest))
		return
	}

	if body.FitnessGoals != nil {
		for _, g := range *body.FitnessGoals {
			if !g.Valid() {
				c.JSON(http.StatusBadRequest, api.NewError("invalid fitness goal: "+string(g), api.CodeBadRequest))
				return
			}
		}
	}

	name := ""
	if body.Name != nil {
		name = *body.Name
	}

	gender := ""
	if body.Gender != nil {
		gender = string(*body.Gender)
	}

	// nil slice → SQL NULL → COALESCE leaves existing goals intact
	var fitnessGoals []string
	if body.FitnessGoals != nil {
		fitnessGoals = make([]string, 0, len(*body.FitnessGoals))
		for _, g := range *body.FitnessGoals {
			fitnessGoals = append(fitnessGoals, string(g))
		}
	}

	fitnessLevel := ""
	if body.FitnessLevel != nil {
		fitnessLevel = string(*body.FitnessLevel)
	}

	avatarURL := ""
	if body.AvatarUrl != nil {
		avatarURL = *body.AvatarUrl
	}

	updated, err := s.users.q.UpdateUserOnboarding(c.Request.Context(), db.UpdateUserOnboardingParams{
		ID:           userID,
		Name:         name,
		Gender:       gender,
		FitnessGoals: fitnessGoals,
		FitnessLevel: fitnessLevel,
		AvatarUrl:    avatarURL,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewError("user not found", api.CodeNotFound))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to update profile", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("Profile updated successfully", api.CodeOK, userToProfileMap(updated)))
}

// GET /users/me/profile
func (s *routerImpl) GetUserProfile(c *gin.Context) {
	if s.users == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}

	user, err := s.users.q.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewError("user not found", api.CodeNotFound))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to fetch profile", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("Profile fetched", api.CodeOK, userToProfileMap(user)))
}
