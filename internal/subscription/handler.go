package subscription

import (
	"net/http"

	"log/slog"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	infra_stripe "github.com/hngprojects/personal-trainer-be/internal/infra/stripe"
)

type Handler struct {
	log    *slog.Logger
	stripe *infra_stripe.Client
}

func NewHandler(log *slog.Logger, stripe *infra_stripe.Client) *Handler {
	return &Handler{log: log, stripe: stripe}
}

func (h *Handler) ListSubscriptions(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, api.NewError("not implemented", api.CodeNotFound))
}

func (h *Handler) CreateSubscription(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, api.NewError("not implemented", api.CodeNotFound))
}

func (h *Handler) GetSubscription(c *gin.Context, id openapi_types.UUID) {
	c.JSON(http.StatusNotImplemented, api.NewError("not implemented", api.CodeNotFound))
}

func (h *Handler) CancelSubscription(c *gin.Context, id openapi_types.UUID) {
	c.JSON(http.StatusNotImplemented, api.NewError("not implemented", api.CodeNotFound))
}

func (h *Handler) ListPayments(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, api.NewError("not implemented", api.CodeNotFound))
}

func (h *Handler) GetPayment(c *gin.Context, id openapi_types.UUID) {
	c.JSON(http.StatusNotImplemented, api.NewError("not implemented", api.CodeNotFound))
}

func (h *Handler) TipTrainer(c *gin.Context, id openapi_types.UUID) {
	c.JSON(http.StatusNotImplemented, api.NewError("not implemented", api.CodeNotFound))
}
