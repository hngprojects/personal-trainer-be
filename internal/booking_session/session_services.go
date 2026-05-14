package booking_session

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

const (
	sessionBooked    = "booked"
	sessionStarted   = "started"
	sessionActive    = "in-session"
	sessionCompleted = "completed"
)

type SessionInterface interface {
	GetSessionById(ctx context.Context, sessionID uuid.UUID) (*db.BookingSession, error)
	StartSession(ctx context.Context, sessionID uuid.UUID) (*db.BookingSession, error)
	JoinSession(ctx context.Context, sessionID uuid.UUID) (*db.BookingSession, error)
	CompleteSession(ctx context.Context, sessionID uuid.UUID) (*db.BookingSession, error)
	TrainerSessionNote(ctx context.Context, sessionID uuid.UUID, notes string) (*db.BookingSession, error)
}

type sessionService struct {
	repo BookingSessionRepo
	log  *slog.Logger
}

func NewSessionService(sessionRepo BookingSessionRepo, log *slog.Logger) SessionInterface {
	return &sessionService{repo: sessionRepo, log: log}
}

func (r *sessionService) GetSessionById(ctx context.Context, sessionID uuid.UUID) (*db.BookingSession, error) {
	dbData, err := r.repo.GetSessionByID(ctx, sessionID)
	if err != nil {
		r.log.Error("error getting session by ID", "err", err)
		return nil, err
	}
	return dbData, nil
}

func (r *sessionService) StartSession(ctx context.Context, sessionID uuid.UUID) (*db.BookingSession, error) {
	session, err := r.repo.GetSessionByID(ctx, sessionID)
	if err != nil {
		r.log.Error("failed to get session", "err", err)
		return nil, err
	}
	switch session.Status.String {
	case sessionStarted:
		return nil, errors.New("session already started")
	case sessionActive:
		return nil, errors.New("session already in-session")
	case sessionCompleted:
		return nil, errors.New("session already completed")
	}
	updatedSession, err := r.repo.MarkSessionAsStarted(ctx, sessionID)
	if err != nil {
		r.log.Error("failed to mark session as started", "err", err)
		return nil, err
	}
	return updatedSession, nil
}

func (r *sessionService) JoinSession(ctx context.Context, sessionID uuid.UUID) (*db.BookingSession, error) {
	session, err := r.repo.GetSessionByID(ctx, sessionID)
	if err != nil {
		r.log.Error("failed to get session", "err", err)
		return nil, err
	}
	switch session.Status.String {
	case sessionBooked:
		return nil, errors.New("trainer has not started session yet")
	case sessionActive:
		return nil, errors.New("session already in-session")
	case sessionCompleted:
		return nil, errors.New("session already completed")
	}
	updatedSession, err := r.repo.MarkSessionAsJoined(ctx, sessionID)
	if err != nil {
		r.log.Error("failed to join session", "err", err)
		return nil, err
	}
	return updatedSession, nil
}

func (r *sessionService) CompleteSession(ctx context.Context, sessionID uuid.UUID) (*db.BookingSession, error) {
	session, err := r.repo.GetSessionByID(ctx, sessionID)
	if err != nil {
		r.log.Error("failed to get session", "err", err)
		return nil, err
	}
	switch session.Status.String {
	case sessionStarted:
		return nil, errors.New("not yet in session")
	case sessionBooked:
		return nil, errors.New("session has not started")
	case sessionCompleted:
		return nil, errors.New("session already completed")
	}
	updatedSession, err := r.repo.MarkSessionAsCompleted(ctx, sessionID)
	if err != nil {
		r.log.Error("failed to complete session", "err", err)
		return nil, err
	}
	return updatedSession, nil
}

func (r *sessionService) TrainerSessionNote(ctx context.Context, sessionID uuid.UUID, notes string) (*db.BookingSession, error) {
	session, err := r.repo.GetSessionByID(ctx, sessionID)
	if err != nil {
		r.log.Error("failed to get session", "err", err)
		return nil, err
	}
	if session.Status.String != sessionCompleted {
		return nil, errors.New("session is not completed yet")
	}
	updatedSession, err := r.repo.UpdateTrainersNote(ctx, sessionID, notes)
	if err != nil {
		r.log.Error("failed to complete session", "err", err)
		return nil, err
	}
	return updatedSession, nil
}
