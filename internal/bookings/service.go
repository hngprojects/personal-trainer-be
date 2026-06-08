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
	activeSubscription = "active"
	// Platforms that go through meeting.Provider — i.e. produce a join
	// URL the email/UI renders. Messenger is intentionally absent: it
	// stores a client-supplied handle on the booking row and the
	// trainer initiates contact via Facebook — no server-minted URL.
	meetingPlatforms = map[string]bool{
		"zoom":        true,
		"google_meet": true,
	}
)

func isMeetingPlatform(p string) bool { return meetingPlatforms[p] }

type BookingSlotService interface {
	GetTrainersBookingSlots(ctx context.Context, trainerId uuid.UUID) ([]db.GetTrainersBookingSlotsRow, error)
	// GetTrainersBookingSlotsForDate returns the trainer's template
	// slots for the weekday of `targetDate`, with any slot already
	// booked on that date (paid session OR discovery call for the
	// same trainer) excluded. Used by the slot picker UI to show only
	// genuinely-available windows for the calendar day the user is
	// looking at.
	GetTrainersBookingSlotsForDate(ctx context.Context, trainerId uuid.UUID, targetDate time.Time) ([]db.GetTrainersBookingSlotsRow, error)
}
type BookingService interface {
	CreateBooking(ctx context.Context, args db.CreateBookingParams, user db.User, trainer db.GetTrainerUserDetailsRow) (*db.Booking, error)
	GetTrainerDetails(ctx context.Context, id uuid.UUID) (*db.GetTrainerUserDetailsRow, error)
	CheckSubscription(ctx context.Context, subID uuid.UUID) (bool, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (*db.User, error)
	CheckBookingConflictForClient(ctx context.Context, arg db.CheckBookingConflictForClientParams) (int64, error)
}

type bookingService struct {
	repo Repository
	// meetings picks the meeting Provider per trainer at call time —
	// trainers who have connected their own Zoom host their own
	// meetings; everyone else falls back to the org account.
	meetings meeting.Selector
	mailer   email.Mailer
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

func NewBookingService(repo Repository, meetingSelector meeting.Selector, mailer email.Mailer, log *slog.Logger, joinMode, universalLinkDomain string) BookingService {
	return &bookingService{
		repo:      repo,
		meetings:  meetingSelector,
		mailer:    mailer,
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

func (s *bookingService) GetTrainersBookingSlotsForDate(ctx context.Context, trainerId uuid.UUID, targetDate time.Time) ([]db.GetTrainersBookingSlotsRow, error) {
	return s.repo.FindBookingSlotByTrainerIDForDate(ctx, trainerId, targetDate)
}

func (s *bookingService) CheckBookingConflictForClient(ctx context.Context, arg db.CheckBookingConflictForClientParams) (int64, error) {
	return s.repo.CheckBookingConflictForClient(ctx, arg)
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
	platform := args.SessionPlatform.String
	if isMeetingPlatform(platform) {
		// trainer.ID is the trainer's user_id (see GetTrainerUserDetails
		// SQL) — that's what the Selector keys per-user grants on. The
		// platform argument routes between Zoom (per-trainer-or-org)
		// and Meet (always org).
		prov := s.meetings.For(ctx, trainer.ID, platform)
		if !prov.IsConfigured() {
			return nil, fmt.Errorf("%s is not configured", platform)
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
			s.log.Error("failed to create meeting", "booking_id", booking.ID, "platform", platform, "err", zoomErr)
			return nil, fmt.Errorf("failed to create %s meeting: %w", platform, zoomErr)
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
		platform,
		meetingURL,
		false,
	); err != nil {
		s.log.Error("failed to send booking confirmation", "error", err)
	}

	if err := s.mailer.SendBookingConfirmation(
		trainer.Email,
		client.Name,
		trainer.Name,
		args.ScheduledStart.Time,
		args.ScheduledEnd.Time,
		args.Timezone.String,
		platform,
		meetingURL,
		true,
	); err != nil {
		s.log.Error("failed to send booking confirmation to trainer", "error", err)
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
