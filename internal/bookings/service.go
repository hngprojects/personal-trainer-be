package bookings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
	"github.com/hngprojects/personal-trainer-be/pkg/meeting"
)

var (
	activeSubscription  = "active"
	zoomSessionPlatform = "zoom"
)

type BookingSlotService interface {
	GetTrainersBookingSlots(ctx context.Context, trainerId uuid.UUID) ([]db.GetTrainersBookingSlotsRow, error)
}
type BookingService interface {
	CreateBooking(ctx context.Context, args db.CreateBookingParams, user db.User, trainer db.GetTrainerUserDetailsRow) (*db.Booking, error)
	GetTrainerDetails(ctx context.Context, id uuid.UUID) (*db.GetTrainerUserDetailsRow, error)
	CheckSubscription(ctx context.Context, subID uuid.UUID) (bool, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (*db.User, error)
}

// Notifier is satisfied by notification.NotificationService.
// Defined locally so the bookings package stays decoupled from the
// notification package.
type Notifier interface {
	SendNotificationToUser(ctx context.Context, userID uuid.UUID, title, message, idempotencyKey string) error
}

type bookingService struct {
	repo Repository
	// meetings picks the meeting Provider per trainer at call time —
	// trainers who have connected their own Zoom host their own
	// meetings; everyone else falls back to the org account.
	meetings meeting.Selector
	mailer   email.Mailer
	notifier Notifier
	log      *slog.Logger
	// joinLinks transforms (raw zoom URL, session id) → the URL we put
	// in the email. Shared with the reschedule handlers so a booking
	// confirmation and its subsequent reschedule confirmation produce
	// the same kind of link instead of one universal + one raw Zoom.
	joinLinks JoinLinkBuilder
}

func NewBookingSlotService(repo Repository, log *slog.Logger) BookingSlotService {
	return &bookingService{
		repo: repo,
		log:  log,
	}
}

func NewBookingService(repo Repository, meetingSelector meeting.Selector, mailer email.Mailer, notifier Notifier, log *slog.Logger, joinMode, universalLinkDomain string) BookingService {
	return &bookingService{
		repo:      repo,
		meetings:  meetingSelector,
		mailer:    mailer,
		notifier:  notifier,
		log:       log,
		joinLinks: JoinLinkBuilder{JoinMode: joinMode, UniversalLinkDomain: universalLinkDomain},
	}
}

func (s *bookingService) GetTrainersBookingSlots(ctx context.Context, trainerId uuid.UUID) ([]db.GetTrainersBookingSlotsRow, error) {
	slots, err := s.repo.FindBookingSlotByTrainerID(ctx, trainerId)
	if err != nil {
		return nil, err
	}
	return slots, nil
}

func (s *bookingService) CreateBooking(ctx context.Context, args db.CreateBookingParams, client db.User, trainer db.GetTrainerUserDetailsRow) (*db.Booking, error) {
	booking, err := s.repo.CreateBooking(ctx, args)
	if err != nil {
		return nil, err
	}

	// Capture the session row so we can build a per-session deep link
	// for the confirmation email when ZoomJoinMode=sdk.
	var sessionID uuid.UUID
	if sess, sessErr := s.repo.CreateBookingSession(ctx, db.CreateBookingSessionParams{
		BookingID:   booking.ID,
		ActualStart: sql.NullTime{},
	}); sessErr != nil {
		s.log.Error("failed to create booking session record", "booking_id", booking.ID, "err", sessErr)
	} else {
		sessionID = sess.ID
	}

	meetingURL := ""
	if args.SessionPlatform.String == zoomSessionPlatform {
		// trainer.ID is the trainer's user_id (see GetTrainerUserDetails
		// SQL) — that's what the Selector keys per-user grants on.
		prov := s.meetings.For(ctx, trainer.ID)
		if !prov.IsConfigured() {
			return nil, fmt.Errorf("zoom is not configured")
		}

		durationMins := 60
		if args.ScheduledStart.Valid && args.ScheduledEnd.Valid {
			computed := int(args.ScheduledEnd.Time.Sub(args.ScheduledStart.Time).Minutes())
			if computed > 0 {
				durationMins = computed
			}
		}

		topic := fmt.Sprintf("Training session with %s", trainer.Name)
		joinURL, meetingID, zoomErr := prov.CreateMeeting(ctx, topic, args.ScheduledStart.Time, durationMins)
		if zoomErr != nil {
			s.log.Error("failed to create zoom meeting", "booking_id", booking.ID, "err", zoomErr)
			return nil, fmt.Errorf("failed to create zoom meeting: %w", zoomErr)
		}

		if _, dbErr := s.repo.UpdateBookingZoom(ctx, db.UpdateBookingZoomParams{
			ID:              booking.ID,
			ZoomMeetingLink: sql.NullString{String: joinURL, Valid: true},
			ZoomMeetingID:   sql.NullString{String: meetingID, Valid: true},
		}); dbErr != nil {
			cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if delErr := prov.DeleteMeeting(cleanCtx, meetingID); delErr != nil {
				s.log.Error("orphaned zoom meeting — manual cleanup required",
					"meeting_id", meetingID, "booking_id", booking.ID, "err", delErr)
			}
			cancel()
			return nil, fmt.Errorf("failed to persist zoom meeting: %w", dbErr)
		}

		meetingURL = joinURL
		booking.ZoomMeetingLink = sql.NullString{String: joinURL, Valid: true}
		booking.ZoomMeetingID = sql.NullString{String: meetingID, Valid: true}
	}

	meetingURL = s.joinLinks.Build(meetingURL, sessionID)

	if err := s.mailer.SendBookingConfirmation(
		client.Email,
		client.Name,
		trainer.Name,
		args.ScheduledStart.Time,
		args.ScheduledEnd.Time,
		args.Timezone.String,
		meetingURL,
	); err != nil {
		s.log.Error("failed to send booking confirmation", "error", err)
	}

	if s.notifier != nil {
		go func() {
			notifCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			if err := s.notifier.SendNotificationToUser(notifCtx, trainer.ID,
				"New Booking",
				"You have a new session booking from "+client.Name,
				"booking-created-"+booking.ID.String(),
			); err != nil {
				s.log.Warn("failed to notify trainer of new booking", "trainerID", trainer.ID, "err", err)
			}
		}()
	}

	return booking, nil
}

func (s *bookingService) GetUserByID(ctx context.Context, id uuid.UUID) (*db.User, error) {
	user, err := s.repo.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *bookingService) CheckSubscription(ctx context.Context, subID uuid.UUID) (bool, error) {
	subscription, err := s.repo.GetSubscriptionDetails(ctx, subID)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	isSubscriptionActive := subscription.Status == activeSubscription
	return isSubscriptionActive, nil
}

func (s *bookingService) GetTrainerDetails(ctx context.Context, id uuid.UUID) (*db.GetTrainerUserDetailsRow, error) {
	trainer, err := s.repo.GetTrainerDetails(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &trainer, nil
}
