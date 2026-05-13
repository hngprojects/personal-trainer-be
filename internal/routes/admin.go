package routes

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
)

func (s *routerImpl) AdminAdd(c *gin.Context) {
	if s.admin == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.admin.AdminAdd(c)
}

func (s *routerImpl) AdminApproveTrainer(c *gin.Context, id openapi_types.UUID) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID := uuid.UUID(id)

	_, err := s.trainers.q.GetTrainerByID(c.Request.Context(), trainerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to fetch trainer", api.CodeServerError))
		return
	}

	updated, err := s.trainers.q.ApproveTrainer(c.Request.Context(), trainerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to approve trainer", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("TRAINER_APPROVED", api.CodeOK, trainerToMap(updated)))
}

func (s *routerImpl) CreateTrainer(c *gin.Context) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.admin.AdminCreateTrainer(c)
}
