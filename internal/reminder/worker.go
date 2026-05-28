package reminder

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
)

// Notifier is the subset of notification.NotificationService used here.
type Notifier interface {
	SendNotificationToUser(ctx context.Context, userID uuid.UUID, title, message, idempotencyKey string) error
}

// Worker polls confirmed bookings every minute and sends a push + email
// reminder to both client and trainer 1 hour before the session starts.
type Worker struct {
	db       *sql.DB
	notifier Notifier
	mailer   email.Mailer
	log      *slog.Logger
	stop     context.CancelFunc
}

func New(db *sql.DB, notifier Notifier, mailer email.Mailer, log *slog.Logger) *Worker {
	return &Worker{db: db, notifier: notifier, mailer: mailer, log: log}
}

func (w *Worker) Start(ctx context.Context) {
	ctx, w.stop = context.WithCancel(ctx)
	go w.run(ctx)
}

func (w *Worker) Stop() {
	if w.stop != nil {
		w.stop()
	}
}

func (w *Worker) run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	w.log.Info("session reminder worker started")

	for {
		select {
		case <-ctx.Done():
			w.log.Info("session reminder worker stopped")
			return
		case <-ticker.C:
			w.sendReminders(ctx)
		}
	}
}

type upcomingBooking struct {
	bookingID      uuid.UUID
	clientID       uuid.UUID
	trainerUserID  uuid.UUID // users(id) resolved via trainers join
	clientEmail    string
	clientName     string
	trainerEmail   string
	trainerName    string
	scheduledStart time.Time
	timezone       string
	zoomLink       string
}

func (w *Worker) sendReminders(ctx context.Context) {
	// Query confirmed bookings starting in [59, 60) minutes from now.
	// Using a half-open 1-minute window that matches the tick interval means
	// each booking appears in at most one tick, preventing duplicate emails.
	// Push notifications are additionally deduplicated by idempotency key.
	const q = `
		SELECT
			b.id,
			b.client_id,
			t.user_id AS trainer_user_id,
			cu.email  AS client_email,
			cu.name   AS client_name,
			tu.email  AS trainer_email,
			tu.name   AS trainer_name,
			b.scheduled_start,
			COALESCE(b.timezone, 'UTC') AS timezone,
			COALESCE(b.zoom_meeting_link, '') AS zoom_link
		FROM bookings b
		JOIN users cu ON cu.id = b.client_id
		JOIN trainers t ON t.id = b.trainer_id
		JOIN users tu ON tu.id = t.user_id
		WHERE b.booking_status = 'confirmed'
		  AND b.scheduled_start >= NOW() + INTERVAL '59 minutes'
		  AND b.scheduled_start <  NOW() + INTERVAL '60 minutes'
	`

	rows, err := w.db.QueryContext(ctx, q)
	if err != nil {
		w.log.Warn("reminder: query failed", "err", err)
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			w.log.Warn("reminder: close rows failed", "err", err)
		}
	}()

	for rows.Next() {
		var b upcomingBooking
		if err := rows.Scan(
			&b.bookingID, &b.clientID, &b.trainerUserID,
			&b.clientEmail, &b.clientName,
			&b.trainerEmail, &b.trainerName,
			&b.scheduledStart, &b.timezone, &b.zoomLink,
		); err != nil {
			w.log.Warn("reminder: scan failed", "err", err)
			continue
		}
		w.notify(ctx, b)
	}
	if err := rows.Err(); err != nil {
		w.log.Warn("reminder: row iteration error", "err", err)
	}
}

func (w *Worker) notify(ctx context.Context, b upcomingBooking) {
	tCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	loc, err := time.LoadLocation(b.timezone)
	if err != nil {
		loc = time.UTC
	}
	sessionTime := b.scheduledStart.In(loc).Format("3:04 PM")
	idBase := "reminder-1h-" + b.bookingID.String()

	// --- push to client ---
	if w.notifier != nil {
		if err := w.notifier.SendNotificationToUser(tCtx, b.clientID,
			"Session Reminder",
			fmt.Sprintf("Your session with Coach %s starts in 1 hour at %s", b.trainerName, sessionTime),
			idBase+"-client",
		); err != nil {
			w.log.Warn("reminder: push to client failed", "bookingID", b.bookingID, "err", err)
		}
	}

	// --- push to trainer ---
	if w.notifier != nil {
		if err := w.notifier.SendNotificationToUser(tCtx, b.trainerUserID,
			"Session Reminder",
			fmt.Sprintf("Your session with %s starts in 1 hour at %s", b.clientName, sessionTime),
			idBase+"-trainer",
		); err != nil {
			w.log.Warn("reminder: push to trainer failed", "bookingID", b.bookingID, "err", err)
		}
	}

	// --- email to client ---
	if w.mailer != nil {
		if err := w.mailer.SendSessionReminder(
			b.clientEmail, b.clientName, b.trainerName,
			b.scheduledStart, b.timezone, b.zoomLink,
		); err != nil {
			w.log.Warn("reminder: email to client failed", "bookingID", b.bookingID, "err", err)
		}
	}

	// --- email to trainer ---
	if w.mailer != nil {
		if err := w.mailer.SendSessionReminderTrainer(
			b.trainerEmail, b.trainerName, b.clientName,
			b.scheduledStart, b.timezone, b.zoomLink,
		); err != nil {
			w.log.Warn("reminder: email to trainer failed", "bookingID", b.bookingID, "err", err)
		}
	}

	w.log.Info("reminder: sent", "bookingID", b.bookingID, "scheduledStart", b.scheduledStart)
}
