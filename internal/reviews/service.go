package reviews

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

const (
	DefaultPageLimit = 20
	MaxPageLimit     = 100
	completedStatus  = "completed"
)

var (
	ErrUserNotFound        = errors.New("user not found")
	ErrClientRoleRequired  = errors.New("client role required")
	ErrInvalidRating       = errors.New("invalid rating")
	ErrInvalidLimit        = errors.New("invalid limit")
	ErrInvalidCursor       = errors.New("invalid cursor")
	ErrBookingNotFound     = errors.New("booking not found")
	ErrBookingForbidden    = errors.New("booking forbidden")
	ErrBookingNotCompleted = errors.New("booking not completed")
	ErrReviewAlreadyExists = errors.New("review already exists")
	ErrTrainerNotFound     = errors.New("trainer not found")
)

type Service struct {
	db  *sql.DB
	q   *db.Queries
	log *slog.Logger
}

type CreateReviewInput struct {
	UserID    uuid.UUID
	BookingID uuid.UUID
	Rating    int
	Review    *string
}

type ListTrainerReviewsInput struct {
	TrainerID uuid.UUID
	Limit     int
	Cursor    *string
}

type ListTrainerReviewsResult struct {
	Reviews    []db.Review
	HasMore    bool
	NextCursor *string
}

type reviewCursor struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

func NewService(dbConn *sql.DB, q *db.Queries, log *slog.Logger) *Service {
	return &Service{
		db:  dbConn,
		q:   q,
		log: log,
	}
}

func (s *Service) CreateReview(ctx context.Context, input CreateReviewInput) (db.Review, error) {
	if input.Rating < 1 || input.Rating > 5 {
		return db.Review{}, ErrInvalidRating
	}

	role, err := s.q.GetUserRoleByID(ctx, input.UserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.Review{}, ErrUserNotFound
		}
		return db.Review{}, err
	}
	if role != "client" {
		return db.Review{}, ErrClientRoleRequired
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return db.Review{}, err
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	qtx := s.q.WithTx(tx)

	booking, err := qtx.GetBookingByIDForUpdate(ctx, input.BookingID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.log.Warn("review submission failed", "booking_id", input.BookingID.String(), "error_code", "NOT_FOUND")
			return db.Review{}, ErrBookingNotFound
		}
		s.log.Error("review submission failed", "booking_id", input.BookingID.String(), "error_code", "INTERNAL_ERROR", "err", err)
		return db.Review{}, err
	}
	if booking.ClientUserID != input.UserID {
		s.log.Warn("review submission failed", "trainer_id", booking.TrainerID.String(), "booking_id", booking.ID.String(), "error_code", "FORBIDDEN")
		return db.Review{}, ErrBookingForbidden
	}
	if booking.Status != completedStatus {
		s.log.Warn("review submission failed", "trainer_id", booking.TrainerID.String(), "booking_id", booking.ID.String(), "error_code", "INVALID_INPUT")
		return db.Review{}, ErrBookingNotCompleted
	}

	created, err := qtx.CreateReview(ctx, db.CreateReviewParams{
		BookingID:    booking.ID,
		TrainerID:    booking.TrainerID,
		ClientUserID: booking.ClientUserID,
		Rating:       int32(input.Rating),
		Review:       nullStringPtr(input.Review),
	})
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			s.log.Warn("review submission failed", "trainer_id", booking.TrainerID.String(), "booking_id", booking.ID.String(), "error_code", "CONFLICT")
			return db.Review{}, ErrReviewAlreadyExists
		}
		s.log.Error("review submission failed", "trainer_id", booking.TrainerID.String(), "booking_id", booking.ID.String(), "error_code", "INTERNAL_ERROR", "err", err)
		return db.Review{}, err
	}

	if _, err := qtx.RefreshTrainerReviewStats(ctx, booking.TrainerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.log.Warn("review submission failed", "trainer_id", booking.TrainerID.String(), "booking_id", booking.ID.String(), "error_code", "NOT_FOUND")
			return db.Review{}, ErrTrainerNotFound
		}
		s.log.Error("review submission failed", "trainer_id", booking.TrainerID.String(), "booking_id", booking.ID.String(), "error_code", "INTERNAL_ERROR", "err", err)
		return db.Review{}, err
	}

	if err := tx.Commit(); err != nil {
		s.log.Error("review submission failed", "trainer_id", booking.TrainerID.String(), "booking_id", booking.ID.String(), "error_code", "INTERNAL_ERROR", "err", err)
		return db.Review{}, err
	}
	committed = true

	s.log.Info("review submission succeeded", "trainer_id", booking.TrainerID.String(), "booking_id", booking.ID.String(), "error_code", "OK")
	return created, nil
}

func (s *Service) ListTrainerReviews(ctx context.Context, input ListTrainerReviewsInput) (ListTrainerReviewsResult, error) {
	limit := input.Limit
	if limit == 0 {
		limit = DefaultPageLimit
	}
	if limit < 1 || limit > MaxPageLimit {
		return ListTrainerReviewsResult{}, ErrInvalidLimit
	}

	if _, err := s.q.GetTrainerByID(ctx, input.TrainerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ListTrainerReviewsResult{}, ErrTrainerNotFound
		}
		return ListTrainerReviewsResult{}, err
	}

	fetchLimit := int32(limit + 1)
	var reviews []db.Review
	var err error

	if input.Cursor == nil || *input.Cursor == "" {
		reviews, err = s.q.ListTrainerReviewsFirstPage(ctx, db.ListTrainerReviewsFirstPageParams{
			TrainerID:  input.TrainerID,
			LimitCount: fetchLimit,
		})
	} else {
		cursor, cursorErr := decodeCursor(*input.Cursor)
		if cursorErr != nil {
			return ListTrainerReviewsResult{}, ErrInvalidCursor
		}
		reviews, err = s.q.ListTrainerReviewsAfterCursor(ctx, db.ListTrainerReviewsAfterCursorParams{
			TrainerID:       input.TrainerID,
			CursorCreatedAt: cursor.CreatedAt,
			CursorID:        cursor.ID,
			LimitCount:      fetchLimit,
		})
	}
	if err != nil {
		return ListTrainerReviewsResult{}, err
	}

	result := ListTrainerReviewsResult{
		Reviews: reviews,
	}
	if len(reviews) > limit {
		result.HasMore = true
		result.Reviews = reviews[:limit]
		nextCursor, err := encodeCursor(result.Reviews[len(result.Reviews)-1])
		if err != nil {
			return ListTrainerReviewsResult{}, err
		}
		result.NextCursor = &nextCursor
	}

	return result, nil
}

func encodeCursor(review db.Review) (string, error) {
	payload, err := json.Marshal(reviewCursor{
		ID:        review.ID,
		CreatedAt: review.CreatedAt.UTC(),
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeCursor(raw string) (reviewCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return reviewCursor{}, err
	}

	var cursor reviewCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return reviewCursor{}, err
	}
	if cursor.ID == uuid.Nil || cursor.CreatedAt.IsZero() {
		return reviewCursor{}, ErrInvalidCursor
	}

	return cursor, nil
}

func nullStringPtr(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: *s, Valid: true}
}
