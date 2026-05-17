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

type bookingService struct {
	repo            Repository
	meetingProvider meeting.Provider
	mailer          email.Mailer
	log             *slog.Logger
}

func NewBookingSlotService(repo Repository, log *slog.Logger) BookingSlotService {
	return &bookingService{
		repo: repo,
		log:  log,
	}
}

func NewBookingService(repo Repository, meetingProvider meeting.Provider, mailer email.Mailer, log *slog.Logger) BookingService {
	return &bookingService{
		repo:            repo,
		meetingProvider: meetingProvider,
		mailer:          mailer,
		log:             log,
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
	var meetingURL string
	booking, err := s.repo.CreateBooking(ctx, args)
	if err != nil {
		return nil, err
	}

	if _, sessErr := s.repo.CreateBookingSession(ctx, db.CreateBookingSessionParams{
		BookingID:   booking.ID,
		ActualStart: sql.NullTime{},
	}); sessErr != nil {
		s.log.Error("failed to create booking session record", "booking_id", booking.ID, "err", sessErr)
	}

	meetingURL = ""
	if args.SessionPlatform.String == zoomSessionPlatform {
		if !s.meetingProvider.IsConfigured() {
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
		joinURL, meetingID, zoomErr := s.meetingProvider.CreateMeeting(ctx, topic, args.ScheduledStart.Time, durationMins)
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
			if delErr := s.meetingProvider.DeleteMeeting(cleanCtx, meetingID); delErr != nil {
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
