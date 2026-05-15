package routes

import (
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
)

func (s *routerImpl) PostTrainersApply(c *gin.Context) {
	if s.trainers == nil {
		c.JSON(503, api.NewError("trainer service unavailable", api.CodeServerError))
		return
	}
	s.trainers.TrainerApply(c)
}

func (s *routerImpl) GetTrainersIdReviews(
	c *gin.Context,
	id openapi_types.UUID,
	params api.GetTrainersIdReviewsParams,
) {
	if s.trainers == nil {
		c.JSON(503, gin.H{"error": "trainer service unavailable"})
		return
	}
	s.trainers.GetTrainerReviews(c, id, params)
}

func (s *routerImpl) GetTrainersId(c *gin.Context, id openapi_types.UUID) {
	if s.trainers == nil {
		c.JSON(503, gin.H{"error": "trainer service unavailable"})
		return
	}
	s.trainers.GetTrainerId(c, id)
}

func (s *routerImpl) GetTrainers(c *gin.Context, params api.GetTrainersParams) {
	if s.trainers == nil {
		c.JSON(503, gin.H{"error": "trainer service unavailable"})
		return
	}
	s.trainers.GetTrainers(c, params)
}
