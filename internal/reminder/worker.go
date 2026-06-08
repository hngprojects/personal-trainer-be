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

// Worker polls confirmed bookings every minute and dispatches reminders
// at multiple lead times before the session starts:
//
//   - 60 minutes: full reminder — push + email to both client and trainer.
//   - 30 minutes: push-only nudge so the participants get a final
//     lockscreen ping. No email (would be spam — we already sent one
//     30 minutes ago).
//
// Each tick is keyed independently for idempotency so a flapping worker
// (restart, lock contention) can't double-fire either pass.
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
			w.sendAllReminders(ctx)
		}
	}
}

// reminderTick describes a single lead-time pass: the window we look at
// (60 → query bookings starting [59,60) min from now), the idempotency
// prefix used to dedupe the resulting notifications, the wording, and
// whether to also send email.
type reminderTick struct {
	leadMinutes int
	idemPrefix  string
	headline    string // shown to client; trainer copy substitutes name
	sendEmail   bool   // false for the close-in nudges
}

var reminderTicks = []reminderTick{
	{leadMinutes: 60, idemPrefix: "reminder-1h-", headline: "Session in 1 hour", sendEmail: true},
	{leadMinutes: 30, idemPrefix: "reminder-30m-", headline: "Session in 30 minutes", sendEmail: false},
}

func (w *Worker) sendAllReminders(ctx context.Context) {
	for _, tick := range reminderTicks {
		w.sendReminders(ctx, tick)
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

// sendReminders runs one pass for a given lead-time. The query uses a
// half-open 1-minute window matching the tick interval so each booking
// appears in at most one tick per lead-time — push notifications are
// further deduplicated by idempotency key in case of restart overlap.
func (w *Worker) sendReminders(ctx context.Context, tick reminderTick) {
	q := fmt.Sprintf(`
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
		  AND b.scheduled_start >= NOW() + INTERVAL '%d minutes'
		  AND b.scheduled_start <  NOW() + INTERVAL '%d minutes'
	`, tick.leadMinutes-1, tick.leadMinutes)

	rows, err := w.db.QueryContext(ctx, q)
	if err != nil {
		w.log.Warn("reminder: query failed", "leadMinutes", tick.leadMinutes, "err", err)
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
		w.notify(ctx, b, tick)
	}
	if err := rows.Err(); err != nil {
		w.log.Warn("reminder: row iteration error", "err", err)
	}
}

func (w *Worker) notify(ctx context.Context, b upcomingBooking, tick reminderTick) {
	tCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	loc, err := time.LoadLocation(b.timezone)
	if err != nil {
		loc = time.UTC
	}
	sessionTime := b.scheduledStart.In(loc).Format("3:04 PM")
	idBase := tick.idemPrefix + b.bookingID.String()
	leadCopy := leadMinutesToCopy(tick.leadMinutes)

	// --- push to client ---
	if w.notifier != nil {
		if err := w.notifier.SendNotificationToUser(tCtx, b.clientID,
			tick.headline,
			fmt.Sprintf("Your session with Coach %s starts in %s at %s", b.trainerName, leadCopy, sessionTime),
			idBase+"-client",
		); err != nil {
			w.log.Warn("reminder: push to client failed", "bookingID", b.bookingID, "leadMinutes", tick.leadMinutes, "err", err)
		}
	}

	// --- push to trainer ---
	if w.notifier != nil {
		if err := w.notifier.SendNotificationToUser(tCtx, b.trainerUserID,
			tick.headline,
			fmt.Sprintf("Your session with %s starts in %s at %s", b.clientName, leadCopy, sessionTime),
			idBase+"-trainer",
		); err != nil {
			w.log.Warn("reminder: push to trainer failed", "bookingID", b.bookingID, "leadMinutes", tick.leadMinutes, "err", err)
		}
	}

	if !tick.sendEmail {
		w.log.Info("reminder: push-only sent", "bookingID", b.bookingID, "leadMinutes", tick.leadMinutes, "scheduledStart", b.scheduledStart)
		return
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

	w.log.Info("reminder: sent", "bookingID", b.bookingID, "leadMinutes", tick.leadMinutes, "scheduledStart", b.scheduledStart)
}

// leadMinutesToCopy turns the worker's integer lead-time into the
// short string we drop into the user-facing reminder copy. Specific
// values get hand-crafted phrasing — anything unexpected falls back to
// "N minutes" which is still grammatical.
func leadMinutesToCopy(min int) string {
	switch min {
	case 60:
		return "1 hour"
	case 30:
		return "30 minutes"
	default:
		return fmt.Sprintf("%d minutes", min)
	}
}
