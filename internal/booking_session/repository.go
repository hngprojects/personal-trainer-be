package booking_session

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

var (
	ErrNotFound = auth.ErrNotFound
)

type BookingSessionRepo interface {
	// GetSessionByID returns the session joined with its parent booking so
	// the trainer_id is included — callers can render the trainer alongside
	// the session without a second query.
	GetSessionByID(ctx context.Context, sessionID uuid.UUID) (*db.GetBookingSessionByIdRow, error)
	MarkSessionAsStarted(ctx context.Context, sessionID uuid.UUID, start time.Time) (*db.BookingSession, error)
	MarkSessionAsJoined(ctx context.Context, sessionID uuid.UUID) (*db.BookingSession, error)
	MarkSessionAsCompleted(ctx context.Context, sessionID uuid.UUID, end time.Time) (*db.BookingSession, error)
	UpdateTrainersNote(ctx context.Context, sessionID uuid.UUID, notes string) (*db.BookingSession, error)
}

type bookingSessionRepo struct {
	q *db.Queries
}

func NewPostgresBookingSessionRepo(q *db.Queries) BookingSessionRepo {
	return &bookingSessionRepo{q: q}
}

func (r *bookingSessionRepo) GetSessionByID(ctx context.Context, sessionID uuid.UUID) (*db.GetBookingSessionByIdRow, error) {
	bookingSession, err := r.q.GetBookingSessionById(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &bookingSession, nil
}

func (r *bookingSessionRepo) MarkSessionAsStarted(ctx context.Context, sessionID uuid.UUID, start time.Time) (*db.BookingSession, error) {
	body := &db.MarkSessionAsStartedParams{
		ID:            sessionID,
		ActualStart:   sql.NullTime{Valid: true, Time: start},
		TrainerJoined: sql.NullBool{Valid: true, Bool: true},
		Status:        sessionStarted,
	}
	bookingSession, err := r.q.MarkSessionAsStarted(ctx, *body)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &bookingSession, nil
}

func (r *bookingSessionRepo) MarkSessionAsJoined(ctx context.Context, sessionID uuid.UUID) (*db.BookingSession, error) {
	body := &db.MarkSessionAsJoinedParams{
		ID:           sessionID,
		ClientJoined: sql.NullBool{Valid: true, Bool: true},
		Status:       sessionActive,
	}
	bookingSession, err := r.q.MarkSessionAsJoined(ctx, *body)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &bookingSession, nil
}

func (r *bookingSessionRepo) MarkSessionAsCompleted(ctx context.Context, sessionID uuid.UUID, actualEnd time.Time) (*db.BookingSession, error) {
	body := &db.MarkSessionAsCompletedParams{
		ID:        sessionID,
		ActualEnd: sql.NullTime{Valid: true, Time: actualEnd},
		Status:    sessionCompleted,
	}
	bookingSession, err := r.q.MarkSessionAsCompleted(ctx, *body)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &bookingSession, nil
}

func (r *bookingSessionRepo) UpdateTrainersNote(ctx context.Context, sessionID uuid.UUID, notes string) (*db.BookingSession, error) {
	body := &db.CollectTrainersNoteParams{
		ID:           sessionID,
		TrainerNotes: sql.NullString{Valid: true, String: notes},
	}
	bookingSession, err := r.q.CollectTrainersNote(ctx, *body)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &bookingSession, nil
}
