package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
)

func (s *routerImpl) registerSubscriptionRoutes(rg *gin.RouterGroup) {
	if s.subscription == nil {
		return
	}

	parseID := func(c *gin.Context) (openapi_types.UUID, bool) {
		var id openapi_types.UUID
		if err := id.UnmarshalText([]byte(c.Param("id"))); err != nil {
			c.JSON(http.StatusBadRequest, api.NewError("invalid id", api.CodeBadRequest))
			return id, false
		}
		return id, true
	}

	subs := rg.Group("/subscriptions")
	{
		subs.GET("", s.subscription.ListSubscriptions)
		subs.POST("", s.subscription.CreateSubscription)
		subs.GET("/:id", func(c *gin.Context) {
			id, ok := parseID(c)
			if !ok {
				return
			}
			s.subscription.GetSubscription(c, id)
		})
		subs.PUT("/:id/cancel", func(c *gin.Context) {
			id, ok := parseID(c)
			if !ok {
				return
			}
			s.subscription.CancelSubscription(c, id)
		})
	}

	pays := rg.Group("/payments")
	{
		pays.GET("", s.subscription.ListPayments)
		pays.GET("/:id", func(c *gin.Context) {
			id, ok := parseID(c)
			if !ok {
				return
			}
			s.subscription.GetPayment(c, id)
		})
	}

	rg.POST("/bookings/:id/tip", func(c *gin.Context) { // POST /api/v1/bookings/:id/tip
		id, ok := parseID(c)
		if !ok {
			return
		}
		s.subscription.TipTrainer(c, id)
	})
}
