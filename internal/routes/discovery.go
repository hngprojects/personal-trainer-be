package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
)

func (s *routerImpl) BookDiscoveryCall(c *gin.Context) {
	if s.discovery == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.discovery.BookDiscoveryCall(c)
}

func (s *routerImpl) GetBookingSlots(c *gin.Context, params api.GetBookingSlotsParams) {
	if s.discovery == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.discovery.GetBookingSlots(c, params)
}

func (s *routerImpl) CreateBookingSlot(c *gin.Context) {
	if s.discovery == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.discovery.CreateBookingSlot(c)
}

func (s *routerImpl) UpdateBookingSlot(c *gin.Context, id openapi_types.UUID) {
	if s.discovery == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.discovery.UpdateBookingSlot(c, id)
}

func (s *routerImpl) DeleteBookingSlot(c *gin.Context, id openapi_types.UUID) {
	if s.discovery == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.discovery.DeleteBookingSlot(c, id)
}
