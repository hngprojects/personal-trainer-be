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
	// profile_complete intentionally does NOT require AvatarUrl. Avatars
	// are set exclusively via POST /users/me/profile/picture (which
	// uploads through MinIO and writes the column asynchronously), so
	// gating completion on AvatarUrl.Valid would block clients who
	// finished onboarding via the JSON profile endpoint but haven't
	// uploaded a picture yet — they'd appear "incomplete" forever even
	// though their profile data is fully filled in.
	profileComplete :=
		u.Name != "" &&
			u.Gender.Valid &&
			u.FitnessLevel.Valid
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
		s.logger.Warn("update user profile: users store not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	userID, ok := userIDFromContext(c)
	if !ok {
		s.logger.Warn("update user profile: missing authenticated user")
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}

	var body api.UpdateProfileRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		s.logger.Warn("update user profile: invalid request body", "userID", userID.String(), "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	if body.Gender != nil && !body.Gender.Valid() {
		s.logger.Warn("update user profile: invalid gender", "userID", userID.String())
		c.JSON(http.StatusBadRequest, api.NewError("gender must be male, female, or other", api.CodeBadRequest))
		return
	}

	if body.FitnessLevel != nil && !body.FitnessLevel.Valid() {
		s.logger.Warn("update user profile: invalid fitness level", "userID", userID.String())
		c.JSON(http.StatusBadRequest, api.NewError("fitness_level must be beginner, intermediate, or advanced", api.CodeBadRequest))
		return
	}

	if body.FitnessGoals != nil {
		for _, g := range *body.FitnessGoals {
			if !g.Valid() {
				s.logger.Warn("update user profile: invalid fitness goal", "userID", userID.String(), "goal", string(g))
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

	// avatar_url is intentionally not read from the request body. Avatars
	// are set exclusively via POST /users/me/profile/picture so this
	// endpoint can't accidentally clobber a freshly-uploaded URL. We
	// still pass an empty string to UpdateUserOnboarding because the SQL
	// uses COALESCE(NULLIF(...,''), avatar_url) which preserves the
	// existing value when blank.
	updated, err := s.users.q.UpdateUserOnboarding(c.Request.Context(), db.UpdateUserOnboardingParams{
		ID:           userID,
		Name:         name,
		Gender:       gender,
		FitnessGoals: fitnessGoals,
		FitnessLevel: fitnessLevel,
		AvatarUrl:    "",
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.logger.Warn("update user profile: user not found", "userID", userID.String(), "err", err)
			c.JSON(http.StatusNotFound, api.NewError("user not found", api.CodeNotFound))
			return
		}
		s.logger.Warn("update user profile: failed to update", "userID", userID.String(), "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to update profile", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("Profile updated successfully", api.CodeOK, userToProfileMap(updated)))
}

// GET /users/me/profile
func (s *routerImpl) GetUserProfile(c *gin.Context) {
	if s.users == nil {
		s.logger.Warn("get user profile: users store not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	userID, ok := userIDFromContext(c)
	if !ok {
		s.logger.Warn("get user profile: missing authenticated user")
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}

	user, err := s.users.q.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.logger.Warn("get user profile: user not found", "userID", userID.String(), "err", err)
			c.JSON(http.StatusNotFound, api.NewError("user not found", api.CodeNotFound))
			return
		}
		s.logger.Warn("get user profile: failed to fetch", "userID", userID.String(), "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to fetch profile", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("Profile fetched", api.CodeOK, userToProfileMap(user)))
}

// POST /users/me/deactivate
func (s *routerImpl) DeactivateMyAccount(c *gin.Context) {
	if s.users == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}

	_, err := s.users.q.DeactivateSelf(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusConflict, api.NewError("account is already deactivated", api.CodeConflict))
			return
		}
		s.logger.Error("deactivate account: db error", "userID", userID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to deactivate account", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("account deactivated successfully", api.CodeOK, nil))
}

// POST /users/me/reactivate
// This endpoint is intentionally exempt from DeactivatedMiddleware so
// deactivated users can reach it after logging in.
func (s *routerImpl) ReactivateMyAccount(c *gin.Context) {
	if s.users == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}

	_, err := s.users.q.ReactivateSelf(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusConflict, api.NewError("account is already active", api.CodeConflict))
			return
		}
		s.logger.Error("reactivate account: db error", "userID", userID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to reactivate account", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("account reactivated successfully", api.CodeOK, nil))
}

// DELETE /users/me
// Permanently deletes the authenticated user's account and all their data.
// This action is irreversible.
func (s *routerImpl) DeleteMyAccount(c *gin.Context) {
	if s.users == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}

	// Verify the account exists and get the role before attempting deletion.
	user, err := s.users.q.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewError("account not found", api.CodeNotFound))
			return
		}
		s.logger.Error("delete account: db lookup error", "userID", userID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to delete account", api.CodeServerError))
		return
	}
	if user.Role != "client" {
		c.JSON(http.StatusForbidden, api.NewError("only client accounts can be self-deleted", api.CodeForbidden))
		return
	}

	rows, err := s.users.q.HardDeleteClient(c.Request.Context(), userID)
	if err != nil {
		s.logger.Error("delete account: db error", "userID", userID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to delete account", api.CodeServerError))
		return
	}
	if rows == 0 {
		c.JSON(http.StatusNotFound, api.NewError("account not found", api.CodeNotFound))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("account permanently deleted", api.CodeOK, nil))
}
