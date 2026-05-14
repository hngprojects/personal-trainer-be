package routes

import (
	openapi_types "github.com/oapi-codegen/runtime/types"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
)

func (s *routerImpl) PostTrainersApply(c *gin.Context) {
	if s.trainers == nil {
		c.JSON(503, gin.H{"error": "trainer service unavailable"})
		return
	}
	s.trainers.TrainerApply(c)
}

func (s *routerImpl) GetTrainersIdReviews(
	c *gin.Context,
	id openapi_types.UUID,
	params api.GetTrainersIdReviewsParams,
) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}
