package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
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

	s.admin.AdminApproveTrainer(c, id.String())
}

// admin/trainers -> list of trainer applications
func (s *routerImpl) AdminTrainers(c *gin.Context) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.admin.AdminTrainers(c)
}
