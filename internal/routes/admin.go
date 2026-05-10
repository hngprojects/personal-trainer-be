package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/api"
)

func (s *routerImpl) AdminAdd(c *gin.Context) {
	if s.admin == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.admin.AdminAdd(c)
}

func (s *routerImpl) AdminUpdateRole(c *gin.Context, id uuid.UUID) {
	if s.admin == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.admin.AdminUpdateRole(c, id)
}
