package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
)

func (s *routerImpl) CreateReview(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, api.NewError("review submission not implemented", api.CodeNotImplemented))
}

func (s *routerImpl) GetTrainerReviews(c *gin.Context, _ openapi_types.UUID, _ api.GetTrainerReviewsParams) {
	c.JSON(http.StatusNotImplemented, api.NewError("trainer reviews not implemented", api.CodeNotImplemented))
}
