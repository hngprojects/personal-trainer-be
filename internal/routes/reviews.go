package routes

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	reviewsvc "github.com/hngprojects/personal-trainer-be/internal/reviews"
)

func (s *routerImpl) CreateReview(c *gin.Context) {
	if s.reviews == nil {
		s.logger.Warn("CreateReview: reviews service is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeInternalError))
		return
	}

	userIDValue, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		s.logger.Warn("CreateReview: missing authenticated user in context")
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}

	userID, ok := userIDValue.(uuid.UUID)
	if !ok {
		s.logger.Warn("CreateReview: invalid user id type in context")
		c.JSON(http.StatusUnauthorized, api.NewError("invalid authenticated user", api.CodeUnauthorized))
		return
	}

	var body api.CreateReviewRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		s.logger.Warn("CreateReview: invalid request body", "err", err)
		c.JSON(http.StatusUnprocessableEntity, api.NewError("invalid request body", api.CodeInvalidInput))
		return
	}

	review, err := s.reviews.CreateReview(c.Request.Context(), reviewsvc.CreateReviewInput{
		UserID:    userID,
		BookingID: uuid.UUID(body.BookingId),
		Rating:    body.Rating,
		Review:    body.Review,
	})
	if err != nil {
		status, code, message := mapReviewError(err)
		s.logger.Warn("CreateReview: review creation failed", "err", err)
		c.JSON(status, api.NewError(message, code))
		return
	}

	c.JSON(http.StatusCreated, api.NewSuccess("Review created", api.CodeCreated, reviewToAPI(review)))
}

func (s *routerImpl) GetTrainerReviews(c *gin.Context, id openapi_types.UUID, params api.GetTrainerReviewsParams) {
	if s.reviews == nil {
		s.logger.Warn("GetTrainerReviews: reviews service is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeInternalError))
		return
	}

	limit := 0
	if params.Limit != nil {
		limit = *params.Limit
	}

	result, err := s.reviews.ListTrainerReviews(c.Request.Context(), reviewsvc.ListTrainerReviewsInput{
		TrainerID: uuid.UUID(id),
		Limit:     limit,
		Cursor:    params.Cursor,
	})
	if err != nil {
		status, code, message := mapReviewError(err)
		s.logger.Warn("GetTrainerReviews: listing reviews failed", "trainer_id", id, "err", err)
		c.JSON(status, api.NewError(message, code))
		return
	}

	items := make([]api.Review, 0, len(result.Reviews))
	for _, review := range result.Reviews {
		items = append(items, reviewToAPI(review))
	}

	c.JSON(http.StatusOK, api.NewSuccessWithMeta("Trainer reviews fetched", api.CodeOK, items, api.CursorPaginationMeta{
		HasMore:    result.HasMore,
		NextCursor: result.NextCursor,
	}))
}

func reviewToAPI(review db.Review) api.Review {
	var text *string
	if review.Review.Valid {
		text = &review.Review.String
	}

	return api.Review{
		Id:           openapi_types.UUID(review.ID),
		BookingId:    openapi_types.UUID(review.BookingID),
		TrainerId:    openapi_types.UUID(review.TrainerID),
		ClientUserId: openapi_types.UUID(review.ClientUserID),
		Rating:       int(review.Rating),
		Review:       text,
		CreatedAt:    review.CreatedAt,
		UpdatedAt:    review.UpdatedAt,
	}
}

func mapReviewError(err error) (int, string, string) {
	switch {
	case errors.Is(err, reviewsvc.ErrUserNotFound):
		return http.StatusUnauthorized, api.CodeUnauthorized, "user not found"
	case errors.Is(err, reviewsvc.ErrClientRoleRequired):
		return http.StatusForbidden, api.CodeForbidden, "client role required"
	case errors.Is(err, reviewsvc.ErrInvalidRating):
		return http.StatusUnprocessableEntity, api.CodeInvalidInput, "rating must be between 1 and 5"
	case errors.Is(err, reviewsvc.ErrInvalidLimit):
		return http.StatusUnprocessableEntity, api.CodeInvalidInput, "limit must be between 1 and 100"
	case errors.Is(err, reviewsvc.ErrInvalidCursor):
		return http.StatusUnprocessableEntity, api.CodeInvalidInput, "invalid pagination cursor"
	case errors.Is(err, reviewsvc.ErrBookingNotFound):
		return http.StatusNotFound, api.CodeNotFound, "booking not found"
	case errors.Is(err, reviewsvc.ErrBookingForbidden):
		return http.StatusForbidden, api.CodeForbidden, "booking does not belong to authenticated client"
	case errors.Is(err, reviewsvc.ErrBookingNotCompleted):
		return http.StatusUnprocessableEntity, api.CodeInvalidInput, "booking is not completed"
	case errors.Is(err, reviewsvc.ErrReviewAlreadyExists):
		return http.StatusConflict, api.CodeConflict, "review already exists for this booking"
	case errors.Is(err, reviewsvc.ErrTrainerNotFound):
		return http.StatusNotFound, api.CodeNotFound, "trainer not found"
	default:
		return http.StatusInternalServerError, api.CodeInternalError, "internal server error"
	}
}
