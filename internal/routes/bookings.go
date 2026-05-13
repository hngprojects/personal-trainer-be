package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/api"
)

func (s *routerImpl) BookDiscoveryCall(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeInternalError))
}
