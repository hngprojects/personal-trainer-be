package routes

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func (r *routerImpl) ListSubscriptions(c *gin.Context) {
	r.subscription.ListSubscriptions(c)
}

func (r *routerImpl) CreateSubscription(c *gin.Context) {
	r.subscription.CreateSubscription(c)
}

func (r *routerImpl) GetSubscription(c *gin.Context, id openapi_types.UUID) {
	r.subscription.GetSubscription(c, id)
}

func (r *routerImpl) CancelSubscription(c *gin.Context, id openapi_types.UUID) {
	r.subscription.CancelSubscription(c, id)
}

func (r *routerImpl) ListPayments(c *gin.Context) {
	r.subscription.ListPayments(c)
}

func (r *routerImpl) GetPayment(c *gin.Context, id openapi_types.UUID) {
	r.subscription.GetPayment(c, id)
}

func (r *routerImpl) TipTrainer(c *gin.Context, id openapi_types.UUID) {
	r.subscription.TipTrainer(c, id)
}
