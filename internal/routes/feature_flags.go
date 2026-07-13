package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/api"
)

// GetPublicFeatureFlags handles GET /api/v1/feature-flags — delegated to
// the feature_flags handler when constructed. Stays a 503 stub when the
// service couldn't be built (Redis missing, etc.) instead of panicking,
// matching the rest of the codebase's nil-safe pattern.
func (s *routerImpl) GetPublicFeatureFlags(c *gin.Context) {
	if s.featureFlags == nil {
		s.logger.Warn("get public feature flags: handler not wired")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.featureFlags.GetPublicFlags(c)
}

func (s *routerImpl) ListAdminFeatureFlags(c *gin.Context) {
	if s.featureFlags == nil {
		s.logger.Warn("list admin feature flags: handler not wired")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.featureFlags.ListAdminFlags(c)
}

func (s *routerImpl) SetAdminFeatureFlag(c *gin.Context, key string) {
	if s.featureFlags == nil {
		s.logger.Warn("set admin feature flag: handler not wired")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	_ = key // path param already on c.Param("key"); handler reads it directly
	s.featureFlags.SetFlag(c)
}
