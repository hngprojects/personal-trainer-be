package booking_session_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/booking_session"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

// mockSessionRepo implements booking_session.BookingSessionRepo
type mockSessionRepo struct {
	getSessionByIDFn         func(ctx context.Context, id uuid.UUID) (*db.BookingSession, error)
	markSessionAsStartedFn   func(ctx context.Context, id uuid.UUID, start time.Time) (*db.BookingSession, error)
	markSessionAsJoinedFn    func(ctx context.Context, id uuid.UUID) (*db.BookingSession, error)
	markSessionAsCompletedFn func(ctx context.Context, id uuid.UUID, end time.Time) (*db.BookingSession, error)
	updateTrainersNoteFn     func(ctx context.Context, id uuid.UUID, notes string) (*db.BookingSession, error)
}

func (m *mockSessionRepo) GetSessionByID(ctx context.Context, id uuid.UUID) (*db.BookingSession, error) {
	if m.getSessionByIDFn != nil {
		return m.getSessionByIDFn(ctx, id)
	}
	return nil, booking_session.ErrNotFound
}

func (m *mockSessionRepo) MarkSessionAsStarted(ctx context.Context, id uuid.UUID, start time.Time) (*db.BookingSession, error) {
	if m.markSessionAsStartedFn != nil {
		return m.markSessionAsStartedFn(ctx, id, start)
	}
	return nil, nil
}

func (m *mockSessionRepo) MarkSessionAsJoined(ctx context.Context, id uuid.UUID) (*db.BookingSession, error) {
	if m.markSessionAsJoinedFn != nil {
		return m.markSessionAsJoinedFn(ctx, id)
	}
	return nil, nil
}

func (m *mockSessionRepo) MarkSessionAsCompleted(ctx context.Context, id uuid.UUID, end time.Time) (*db.BookingSession, error) {
	if m.markSessionAsCompletedFn != nil {
		return m.markSessionAsCompletedFn(ctx, id, end)
	}
	return nil, nil
}

func (m *mockSessionRepo) UpdateTrainersNote(ctx context.Context, id uuid.UUID, notes string) (*db.BookingSession, error) {
	if m.updateTrainersNoteFn != nil {
		return m.updateTrainersNoteFn(ctx, id, notes)
	}
	return nil, nil
}

func testLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func sessionWithStatus(status string) *db.BookingSession {
	return &db.BookingSession{
		ID:        uuid.New(),
		BookingID: uuid.New(),
		Status:    status,
		CreatedAt: time.Now(),
	}
}

// ---------------------------------------------------------------------------
// GetSessionById
// ---------------------------------------------------------------------------

func TestGetSessionById_Success(t *testing.T) {
	id := uuid.New()
	want := sessionWithStatus("booked")
	want.ID = id

	svc := booking_session.NewSessionService(&mockSessionRepo{
		getSessionByIDFn: func(_ context.Context, got uuid.UUID) (*db.BookingSession, error) {
			if got != id {
				t.Fatalf("expected id %v, got %v", id, got)
			}
			return want, nil
		},
	}, testLog())

	got, err := svc.GetSessionById(context.Background(), id)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.ID != id {
		t.Errorf("expected id %v, got %v", id, got.ID)
	}
}

func TestGetSessionById_NotFound(t *testing.T) {
	svc := booking_session.NewSessionService(&mockSessionRepo{}, testLog())

	_, err := svc.GetSessionById(context.Background(), uuid.New())
	if !errors.Is(err, booking_session.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// StartSession
// ---------------------------------------------------------------------------

func TestStartSession_Success(t *testing.T) {
	id := uuid.New()
	started := sessionWithStatus("started")

	svc := booking_session.NewSessionService(&mockSessionRepo{
		getSessionByIDFn: func(_ context.Context, _ uuid.UUID) (*db.BookingSession, error) {
			return sessionWithStatus("booked"), nil
		},
		markSessionAsStartedFn: func(_ context.Context, _ uuid.UUID, _ time.Time) (*db.BookingSession, error) {
			return started, nil
		},
	}, testLog())

	got, err := svc.StartSession(context.Background(), id)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Status != "started" {
		t.Errorf("expected status 'started', got %q", got.Status)
	}
}

func TestStartSession_AlreadyStarted(t *testing.T) {
	svc := booking_session.NewSessionService(&mockSessionRepo{
		getSessionByIDFn: func(_ context.Context, _ uuid.UUID) (*db.BookingSession, error) {
			return sessionWithStatus("started"), nil
		},
	}, testLog())

	_, err := svc.StartSession(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error for already-started session")
	}
}

func TestStartSession_AlreadyInSession(t *testing.T) {
	svc := booking_session.NewSessionService(&mockSessionRepo{
		getSessionByIDFn: func(_ context.Context, _ uuid.UUID) (*db.BookingSession, error) {
			return sessionWithStatus("in-session"), nil
		},
	}, testLog())

	_, err := svc.StartSession(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error for in-session")
	}
}

func TestStartSession_AlreadyCompleted(t *testing.T) {
	svc := booking_session.NewSessionService(&mockSessionRepo{
		getSessionByIDFn: func(_ context.Context, _ uuid.UUID) (*db.BookingSession, error) {
			return sessionWithStatus("completed"), nil
		},
	}, testLog())

	_, err := svc.StartSession(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error for completed session")
	}
}

func TestStartSession_NotFound(t *testing.T) {
	svc := booking_session.NewSessionService(&mockSessionRepo{}, testLog())

	_, err := svc.StartSession(context.Background(), uuid.New())
	if !errors.Is(err, booking_session.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// JoinSession
// ---------------------------------------------------------------------------

func TestJoinSession_Success(t *testing.T) {
	inSession := sessionWithStatus("in-session")

	svc := booking_session.NewSessionService(&mockSessionRepo{
		getSessionByIDFn: func(_ context.Context, _ uuid.UUID) (*db.BookingSession, error) {
			return sessionWithStatus("started"), nil
		},
		markSessionAsJoinedFn: func(_ context.Context, _ uuid.UUID) (*db.BookingSession, error) {
			return inSession, nil
		},
	}, testLog())

	got, err := svc.JoinSession(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Status != "in-session" {
		t.Errorf("expected status 'in-session', got %q", got.Status)
	}
}

func TestJoinSession_TrainerNotStartedYet(t *testing.T) {
	svc := booking_session.NewSessionService(&mockSessionRepo{
		getSessionByIDFn: func(_ context.Context, _ uuid.UUID) (*db.BookingSession, error) {
			return sessionWithStatus("booked"), nil
		},
	}, testLog())

	_, err := svc.JoinSession(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error when trainer has not started session")
	}
}

func TestJoinSession_AlreadyInSession(t *testing.T) {
	svc := booking_session.NewSessionService(&mockSessionRepo{
		getSessionByIDFn: func(_ context.Context, _ uuid.UUID) (*db.BookingSession, error) {
			return sessionWithStatus("in-session"), nil
		},
	}, testLog())

	_, err := svc.JoinSession(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error for already in-session")
	}
}

func TestJoinSession_AlreadyCompleted(t *testing.T) {
	svc := booking_session.NewSessionService(&mockSessionRepo{
		getSessionByIDFn: func(_ context.Context, _ uuid.UUID) (*db.BookingSession, error) {
			return sessionWithStatus("completed"), nil
		},
	}, testLog())

	_, err := svc.JoinSession(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error for completed session")
	}
}

func TestJoinSession_NotFound(t *testing.T) {
	svc := booking_session.NewSessionService(&mockSessionRepo{}, testLog())

	_, err := svc.JoinSession(context.Background(), uuid.New())
	if !errors.Is(err, booking_session.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// CompleteSession
// ---------------------------------------------------------------------------

func TestCompleteSession_Success(t *testing.T) {
	completed := sessionWithStatus("completed")

	svc := booking_session.NewSessionService(&mockSessionRepo{
		getSessionByIDFn: func(_ context.Context, _ uuid.UUID) (*db.BookingSession, error) {
			return sessionWithStatus("in-session"), nil
		},
		markSessionAsCompletedFn: func(_ context.Context, _ uuid.UUID, _ time.Time) (*db.BookingSession, error) {
			return completed, nil
		},
	}, testLog())

	got, err := svc.CompleteSession(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", got.Status)
	}
}

func TestCompleteSession_NotInSession(t *testing.T) {
	svc := booking_session.NewSessionService(&mockSessionRepo{
		getSessionByIDFn: func(_ context.Context, _ uuid.UUID) (*db.BookingSession, error) {
			return sessionWithStatus("started"), nil
		},
	}, testLog())

	_, err := svc.CompleteSession(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error when status is 'started' (not yet in-session)")
	}
}

func TestCompleteSession_StillBooked(t *testing.T) {
	svc := booking_session.NewSessionService(&mockSessionRepo{
		getSessionByIDFn: func(_ context.Context, _ uuid.UUID) (*db.BookingSession, error) {
			return sessionWithStatus("booked"), nil
		},
	}, testLog())

	_, err := svc.CompleteSession(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error for booked session")
	}
}

func TestCompleteSession_AlreadyCompleted(t *testing.T) {
	svc := booking_session.NewSessionService(&mockSessionRepo{
		getSessionByIDFn: func(_ context.Context, _ uuid.UUID) (*db.BookingSession, error) {
			return sessionWithStatus("completed"), nil
		},
	}, testLog())

	_, err := svc.CompleteSession(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error for already completed session")
	}
}

func TestCompleteSession_NotFound(t *testing.T) {
	svc := booking_session.NewSessionService(&mockSessionRepo{}, testLog())

	_, err := svc.CompleteSession(context.Background(), uuid.New())
	if !errors.Is(err, booking_session.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// TrainerSessionNote
// ---------------------------------------------------------------------------

func TestTrainerSessionNote_Success(t *testing.T) {
	note := "Great progress today"
	result := sessionWithStatus("completed")

	svc := booking_session.NewSessionService(&mockSessionRepo{
		getSessionByIDFn: func(_ context.Context, _ uuid.UUID) (*db.BookingSession, error) {
			return sessionWithStatus("completed"), nil
		},
		updateTrainersNoteFn: func(_ context.Context, _ uuid.UUID, n string) (*db.BookingSession, error) {
			if n != note {
				t.Errorf("expected note %q, got %q", note, n)
			}
			return result, nil
		},
	}, testLog())

	got, err := svc.TrainerSessionNote(context.Background(), uuid.New(), note)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Status != "completed" {
		t.Errorf("expected completed session, got %q", got.Status)
	}
}

func TestTrainerSessionNote_SessionNotCompleted(t *testing.T) {
	for _, status := range []string{"booked", "started", "in-session"} {
		s := status
		t.Run(s, func(t *testing.T) {
			svc := booking_session.NewSessionService(&mockSessionRepo{
				getSessionByIDFn: func(_ context.Context, _ uuid.UUID) (*db.BookingSession, error) {
					return sessionWithStatus(s), nil
				},
			}, testLog())

			_, err := svc.TrainerSessionNote(context.Background(), uuid.New(), "some note")
			if err == nil {
				t.Fatalf("expected error for status %q — notes only allowed after completion", s)
			}
		})
	}
}

func TestTrainerSessionNote_NotFound(t *testing.T) {
	svc := booking_session.NewSessionService(&mockSessionRepo{}, testLog())

	_, err := svc.TrainerSessionNote(context.Background(), uuid.New(), "note")
	if !errors.Is(err, booking_session.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Full state machine flow
// ---------------------------------------------------------------------------

func TestSessionStateMachine_FullFlow(t *testing.T) {
	id := uuid.New()
	status := "booked"

	repo := &mockSessionRepo{
		getSessionByIDFn: func(_ context.Context, _ uuid.UUID) (*db.BookingSession, error) {
			return &db.BookingSession{ID: id, Status: status, CreatedAt: time.Now()}, nil
		},
		markSessionAsStartedFn: func(_ context.Context, _ uuid.UUID, _ time.Time) (*db.BookingSession, error) {
			status = "started"
			return &db.BookingSession{ID: id, Status: status, CreatedAt: time.Now()}, nil
		},
		markSessionAsJoinedFn: func(_ context.Context, _ uuid.UUID) (*db.BookingSession, error) {
			status = "in-session"
			return &db.BookingSession{ID: id, Status: status, CreatedAt: time.Now()}, nil
		},
		markSessionAsCompletedFn: func(_ context.Context, _ uuid.UUID, _ time.Time) (*db.BookingSession, error) {
			status = "completed"
			return &db.BookingSession{ID: id, Status: status, CreatedAt: time.Now()}, nil
		},
		updateTrainersNoteFn: func(_ context.Context, _ uuid.UUID, _ string) (*db.BookingSession, error) {
			return &db.BookingSession{ID: id, Status: status, CreatedAt: time.Now()}, nil
		},
	}
	svc := booking_session.NewSessionService(repo, testLog())
	ctx := context.Background()

	// 1: trainer starts
	if _, err := svc.StartSession(ctx, id); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	// 2: client joins
	if _, err := svc.JoinSession(ctx, id); err != nil {
		t.Fatalf("JoinSession: %v", err)
	}

	// 3: complete
	if _, err := svc.CompleteSession(ctx, id); err != nil {
		t.Fatalf("CompleteSession: %v", err)
	}

	// 4: trainer adds notes
	if _, err := svc.TrainerSessionNote(ctx, id, "Client showed great improvement"); err != nil {
		t.Fatalf("TrainerSessionNote: %v", err)
	}
}
