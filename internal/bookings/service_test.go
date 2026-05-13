package bookings

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

func TestWithRetrySucceedsAfterRetries(t *testing.T) {
	t.Parallel()

	var (
		mu      sync.Mutex
		attempt int
	)

	err := withRetry(context.Background(), 4, time.Millisecond, 2*time.Millisecond, func(context.Context) error {
		mu.Lock()
		defer mu.Unlock()

		attempt++
		if attempt < 3 {
			return errors.New("temporary failure")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected retry success, got error: %v", err)
	}
	if attempt != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempt)
	}
}

func TestWithRetryExhaustedReturnsLastError(t *testing.T) {
	t.Parallel()

	var attempt int
	err := withRetry(context.Background(), 3, time.Millisecond, 2*time.Millisecond, func(context.Context) error {
		attempt++
		return errors.New("still failing")
	})

	if err == nil {
		t.Fatal("expected error after retries are exhausted")
	}
	if attempt != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempt)
	}
}

func TestSendDiscoveryBookingNotificationsEmailRetryExhaustedStillReturnsWarnings(t *testing.T) {
	t.Parallel()

	mailer := &failingDiscoveryMailer{
		clientErr:  errors.New("client smtp failed"),
		trainerErr: errors.New("trainer smtp failed"),
	}

	svc := NewService(
		nil,
		nil,
		nil,
		mailer,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config{
			EmailRetryAttempts:  3,
			EmailRetryBaseDelay: time.Millisecond,
			EmailRetryMaxDelay:  2 * time.Millisecond,
		},
	)

	result := svc.sendDiscoveryBookingNotifications(context.Background(), db.User{
		ID:    uuid.New(),
		Email: "trainer@example.com",
		Name:  "Trainer",
	}, db.User{
		ID:    uuid.New(),
		Email: "client@example.com",
		Name:  "Client",
	}, db.Booking{
		ID:             uuid.New(),
		ScheduledStart: sql.NullTime{Time: time.Now().UTC(), Valid: true},
		Timezone:       sql.NullString{String: "UTC", Valid: true},
	}, "https://zoom.us/j/test")

	if result.ClientEmailSent {
		t.Fatal("expected client email to be marked as failed")
	}
	if result.TrainerEmailSent {
		t.Fatal("expected trainer email to be marked as failed")
	}
	if len(result.Warnings) != 2 {
		t.Fatalf("expected 2 warning messages, got %d", len(result.Warnings))
	}
	if mailer.clientCalls != 3 {
		t.Fatalf("expected 3 retry attempts for client email, got %d", mailer.clientCalls)
	}
	if mailer.trainerCalls != 3 {
		t.Fatalf("expected 3 retry attempts for trainer email, got %d", mailer.trainerCalls)
	}
}

type failingDiscoveryMailer struct {
	clientErr    error
	trainerErr   error
	clientCalls  int
	trainerCalls int
}

func (m *failingDiscoveryMailer) SendVerificationCode(string, string, int) error {
	return nil
}

func (m *failingDiscoveryMailer) SendAdminCredentials(string, string) error {
	return nil
}

func (m *failingDiscoveryMailer) SendPasswordResetCode(string, string, int) error {
	return nil
}

func (m *failingDiscoveryMailer) SendWaitlistConfirmation(string) error {
	return nil
}

func (m *failingDiscoveryMailer) SendDiscoveryBookingConfirmationToClient(string, string, string, string, string) error {
	m.clientCalls++
	return m.clientErr
}

func (m *failingDiscoveryMailer) SendDiscoveryBookingConfirmationToTrainer(string, string, string, string, string) error {
	m.trainerCalls++
	return m.trainerErr
}
