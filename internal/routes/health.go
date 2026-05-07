// routes/health.go
package routes

import (
	"github.com/gin-gonic/gin"
)

func (s *routerImpl) HealthCheck(c *gin.Context) {
	s.health.Check(c)
}
