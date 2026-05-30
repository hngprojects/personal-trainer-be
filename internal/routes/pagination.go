package routes

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/api"
)

// parsePagination normalises the (page, limit) query params used across the
// paginated list endpoints. Defaults to page=1, limit=10 when absent; rejects
// page<1 or limit outside [1,100] with a 400 and returns ok=false so the
// caller can early-return without duplicating the validation boilerplate.
func parsePagination(c *gin.Context, pageParam, limitParam *int, log *slog.Logger) (page, limit int, ok bool) {
	page, limit = 1, 10
	if pageParam != nil {
		if *pageParam < 1 {
			log.Warn("parsePagination: page < 1", "page", *pageParam)
			c.JSON(http.StatusBadRequest, api.NewError("page must be >= 1", api.CodeBadRequest))
			return 0, 0, false
		}
		page = *pageParam
	}
	if limitParam != nil {
		if *limitParam < 1 || *limitParam > 100 {
			log.Warn("parsePagination: limit out of range", "limit", *limitParam)
			c.JSON(http.StatusBadRequest, api.NewError("limit must be between 1 and 100", api.CodeBadRequest))
			return 0, 0, false
		}
		limit = *limitParam
	}
	return page, limit, true
}
