package routes

import (
	"github.com/gin-gonic/gin"
)

func (s *routerImpl) Root(c *gin.Context) {
	s.root.Root(c)
}
