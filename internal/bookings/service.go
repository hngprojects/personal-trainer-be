package bookings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"

	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
	"github.com/hngprojects/personal-trainer-be/pkg/zoom"
)

const (
	slotStatusAvailable         = "available"
	defaultUpgradeURL           = "/pricing"
	defaultEmailRetryAttempts   = 4
	defaultEmailRetryBaseDelay  = 250 * time.Millisecond
	defaultEmailRetryMaxDelay   = time.Second
	defaultDiscoveryCallTopic   = "Discovery Call"
	defaultDiscoveryCallMinutes = 30
)

var (
	ErrTrainerNotFound      = errors.New("trainer not found")
	ErrClientNotFound       = errors.New("client not found")
	ErrClientRoleRequired   = errors.New("client role required")
	ErrSlotNotFound         = errors.New("booking slot not found")
	ErrSlotMismatch         = errors.New("booking slot does not belong to trainer")
	ErrSlotUnavailable      = errors.New("booking slot unavailable")
	ErrDiscoveryAlreadyUsed = errors.New("discovery call already used")
	ErrZoomUnavailable      = errors.New("zoom unavailable")
	ErrBookingCreateFailed  = errors.New("discovery booking create failed")
)

type DiscoveryAlreadyUsedError struct {
	UpgradeURL string
}

func (e *DiscoveryAlreadyUsedError) Error() string {
	return ErrDiscoveryAlreadyUsed.Error()
}

func (e *DiscoveryAlreadyUsedError) Is(target error) bool {
	return target == ErrDiscoveryAlreadyUsed
}

type Config struct {
	UpgradeURL          string
	EmailRetryAttempts  int
	EmailRetryBaseDelay time.Duration
	EmailRetryMaxDelay  time.Duration
}

type Service struct {
	db     *sql.DB
	q      *db.Queries
	zoom   zoom.Client
	mailer email.Mailer
	log    *slog.Logger
	cfg    Config
}

type BookDiscoveryCallInput struct {
	TrainerID uuid.UUID
	ClientID  uuid.UUID
	SlotID    uuid.UUID
	Timezone  string
}

type NotificationResult struct {
	ClientEmailSent  bool
	TrainerEmailSent bool
	Warnings         []string
}

type BookDiscoveryCallResult struct {
	Booking       db.Booking
	Slot          db.BookingSlot
	Meeting       zoom.CreateMeetingResult
	Notifications NotificationResult
}

func NewService(dbConn *sql.DB, q *db.Queries, zoomClient zoom.Client, mailer email.Mailer, log *slog.Logger, cfg Config) *Service {
	if cfg.UpgradeURL == "" {
		cfg.UpgradeURL = defaultUpgradeURL
	}
	if cfg.EmailRetryAttempts <= 0 {
		cfg.EmailRetryAttempts = defaultEmailRetryAttempts
	}
	if cfg.EmailRetryBaseDelay <= 0 {
		cfg.EmailRetryBaseDelay = defaultEmailRetryBaseDelay
	}
	if cfg.EmailRetryMaxDelay <= 0 {
		cfg.EmailRetryMaxDelay = defaultEmailRetryMaxDelay
	}
	if cfg.EmailRetryMaxDelay < cfg.EmailRetryBaseDelay {
		cfg.EmailRetryMaxDelay = cfg.EmailRetryBaseDelay
	}

	return &Service{
		db:     dbConn,
		q:      q,
		zoom:   zoomClient,
		mailer: mailer,
		log:    log,
		cfg:    cfg,
	}
}

func (s *Service) BookDiscoveryCall(ctx context.Context, input BookDiscoveryCallInput) (BookDiscoveryCallResult, error) {
	if s.db == nil || s.q == nil {
		return BookDiscoveryCallResult{}, ErrBookingCreateFailed
	}
	if s.zoom == nil {
		return BookDiscoveryCallResult{}, fmt.Errorf("%w: zoom client is not configured", ErrZoomUnavailable)
	}

	trainer, err := s.q.GetTrainerByID(ctx, input.TrainerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BookDiscoveryCallResult{}, ErrTrainerNotFound
		}
		return BookDiscoveryCallResult{}, err
	}

	client, err := s.q.GetUserByID(ctx, input.ClientID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BookDiscoveryCallResult{}, ErrClientNotFound
		}
		return BookDiscoveryCallResult{}, err
	}
	role, err := s.q.GetUserRoleByID(ctx, input.ClientID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BookDiscoveryCallResult{}, ErrClientNotFound
		}
		return BookDiscoveryCallResult{}, err
	}
	if role != "client" {
		return BookDiscoveryCallResult{}, ErrClientRoleRequired
	}

	trainerUser, err := s.q.GetUserByID(ctx, trainer.UserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BookDiscoveryCallResult{}, ErrTrainerNotFound
		}
		return BookDiscoveryCallResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BookDiscoveryCallResult{}, err
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	qtx := s.q.WithTx(tx)

	slot, err := qtx.GetBookingSlotByIDForUpdate(ctx, input.SlotID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BookDiscoveryCallResult{}, ErrSlotNotFound
		}
		return BookDiscoveryCallResult{}, err
	}
	if slot.TrainerID != input.TrainerID {
		return BookDiscoveryCallResult{}, ErrSlotMismatch
	}
	if slot.Status != slotStatusAvailable {
		return BookDiscoveryCallResult{}, ErrSlotUnavailable
	}

	alreadyUsed, err := qtx.HasDiscoveryBookingForClientTrainer(ctx, db.HasDiscoveryBookingForClientTrainerParams{
		TrainerID: input.TrainerID,
		ClientID:  input.ClientID,
	})
	if err != nil {
		return BookDiscoveryCallResult{}, err
	}
	if alreadyUsed {
		return BookDiscoveryCallResult{}, &DiscoveryAlreadyUsedError{UpgradeURL: s.cfg.UpgradeURL}
	}

	lockedSlot, err := qtx.LockBookingSlotIfAvailable(ctx, db.LockBookingSlotIfAvailableParams{
		LockedBy:  uuid.NullUUID{UUID: input.ClientID, Valid: true},
		SlotID:    slot.ID,
		TrainerID: input.TrainerID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BookDiscoveryCallResult{}, ErrSlotUnavailable
		}
		return BookDiscoveryCallResult{}, err
	}

	meeting, err := s.zoom.CreateMeeting(ctx, zoom.CreateMeetingInput{
		Topic:           defaultDiscoveryCallTopic,
		StartTime:       lockedSlot.StartsAt,
		DurationMinutes: int(lockedSlot.EndsAt.Sub(lockedSlot.StartsAt).Minutes()),
		Timezone:        coalesceTimezone(input.Timezone, lockedSlot.Timezone),
		Agenda:          fmt.Sprintf("Discovery call between %s and %s", client.Name, trainerUser.Name),
	})
	if err != nil {
		s.log.Warn("zoom meeting creation failed for discovery booking", "slot_id", input.SlotID.String(), "trainer_id", input.TrainerID.String(), "client_id", input.ClientID.String(), "err", err)
		return BookDiscoveryCallResult{}, fmt.Errorf("%w: %v", ErrZoomUnavailable, err)
	}

	durationMinutes := int(lockedSlot.EndsAt.Sub(lockedSlot.StartsAt).Minutes())
	if durationMinutes <= 0 {
		durationMinutes = defaultDiscoveryCallMinutes
	}

	booking, err := qtx.CreateDiscoveryBooking(ctx, db.CreateDiscoveryBookingParams{
		TrainerID:       input.TrainerID,
		ClientID:        input.ClientID,
		CalendlyEventID: sql.NullString{String: meeting.MeetingID, Valid: meeting.MeetingID != ""},
		ScheduledStart:  sql.NullTime{Time: lockedSlot.StartsAt, Valid: true},
		ScheduledEnd:    sql.NullTime{Time: lockedSlot.StartsAt.Add(time.Duration(durationMinutes) * time.Minute), Valid: true},
		Timezone: sql.NullString{
			String: coalesceTimezone(input.Timezone, lockedSlot.Timezone),
			Valid:  coalesceTimezone(input.Timezone, lockedSlot.Timezone) != "",
		},
		BookingStatus:      sql.NullString{String: "booked", Valid: true},
		SessionPlatform:    sql.NullString{String: "zoom", Valid: true},
		CancellationReason: sql.NullString{},
		CreatedAt:          sql.NullTime{Time: time.Now().UTC(), Valid: true},
		CancelledAt:        sql.NullTime{},
		MeetingJoinUrl:     sql.NullString{String: meeting.JoinURL, Valid: meeting.JoinURL != ""},
		MeetingStartUrl:    sql.NullString{String: meeting.StartURL, Valid: meeting.StartURL != ""},
	})
	if err != nil {
		if isUniqueViolation(err) {
			return BookDiscoveryCallResult{}, &DiscoveryAlreadyUsedError{UpgradeURL: s.cfg.UpgradeURL}
		}
		return BookDiscoveryCallResult{}, fmt.Errorf("%w: %v", ErrBookingCreateFailed, err)
	}

	bookedSlot, err := qtx.MarkBookingSlotBooked(ctx, db.MarkBookingSlotBookedParams{
		BookingID: uuid.NullUUID{UUID: booking.ID, Valid: true},
		SlotID:    lockedSlot.ID,
	})
	if err != nil {
		return BookDiscoveryCallResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return BookDiscoveryCallResult{}, err
	}
	committed = true

	notifications := s.sendDiscoveryBookingNotifications(ctx, trainerUser, client, booking, meeting.JoinURL)
	result := BookDiscoveryCallResult{
		Booking:       booking,
		Slot:          bookedSlot,
		Meeting:       meeting,
		Notifications: notifications,
	}
	return result, nil
}

func (s *Service) sendDiscoveryBookingNotifications(ctx context.Context, trainerUser db.User, client db.User, booking db.Booking, meetingJoinURL string) NotificationResult {
	if s.mailer == nil {
		return NotificationResult{
			Warnings: []string{"mailer is not configured"},
		}
	}

	scheduledAt := booking.ScheduledStart.Time.UTC().Format(time.RFC3339)
	if booking.ScheduledStart.Valid {
		scheduledAt = booking.ScheduledStart.Time.Format(time.RFC3339)
	}

	timezone := "UTC"
	if booking.Timezone.Valid && booking.Timezone.String != "" {
		timezone = booking.Timezone.String
	}

	result := NotificationResult{}

	err := withRetry(ctx, s.cfg.EmailRetryAttempts, s.cfg.EmailRetryBaseDelay, s.cfg.EmailRetryMaxDelay, func(ctx context.Context) error {
		return s.mailer.SendDiscoveryBookingConfirmationToClient(
			client.Email,
			trainerUser.Name,
			scheduledAt,
			timezone,
			meetingJoinURL,
		)
	})
	if err != nil {
		s.log.Warn("failed to send discovery booking email to client", "client_id", client.ID.String(), "trainer_id", trainerUser.ID.String(), "booking_id", booking.ID.String(), "err", err)
		result.Warnings = append(result.Warnings, "client confirmation email failed")
	} else {
		result.ClientEmailSent = true
	}

	err = withRetry(ctx, s.cfg.EmailRetryAttempts, s.cfg.EmailRetryBaseDelay, s.cfg.EmailRetryMaxDelay, func(ctx context.Context) error {
		return s.mailer.SendDiscoveryBookingConfirmationToTrainer(
			trainerUser.Email,
			client.Name,
			scheduledAt,
			timezone,
			meetingJoinURL,
		)
	})
	if err != nil {
		s.log.Warn("failed to send discovery booking email to trainer", "trainer_id", trainerUser.ID.String(), "client_id", client.ID.String(), "booking_id", booking.ID.String(), "err", err)
		result.Warnings = append(result.Warnings, "trainer confirmation email failed")
	} else {
		result.TrainerEmailSent = true
	}

	return result
}

func withRetry(ctx context.Context, attempts int, baseDelay, maxDelay time.Duration, fn func(context.Context) error) error {
	if attempts <= 0 {
		attempts = 1
	}
	if baseDelay <= 0 {
		baseDelay = defaultEmailRetryBaseDelay
	}
	if maxDelay <= 0 {
		maxDelay = defaultEmailRetryMaxDelay
	}
	if maxDelay < baseDelay {
		maxDelay = baseDelay
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := fn(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}

		if attempt == attempts-1 {
			break
		}

		delay := backoffDelay(baseDelay, maxDelay, attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

func backoffDelay(base, max time.Duration, attempt int) time.Duration {
	delay := base
	for i := 0; i < attempt; i++ {
		if delay >= max/2 {
			return max
		}
		delay *= 2
	}
	if delay > max {
		return max
	}
	return delay
}

func coalesceTimezone(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	if fallback != "" {
		return fallback
	}
	return "UTC"
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return true
	}

	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
