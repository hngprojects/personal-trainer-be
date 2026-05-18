package bookings

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

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
	booking, err := s.repo.CreateBooking(ctx, args)
	if err != nil {
		return nil, err
	}
	if args.SessionPlatform.String == zoomSessionPlatform && s.meetingProvider.IsConfigured() {
		s.log.Info("connecting to zoom.")
	}
	if err := s.mailer.SendBookingConfirmation(
		client.Email,
		client.Name,
		trainer.Name,
		args.ScheduledStart.Time,
		args.ScheduledEnd.Time,
		args.Timezone.String,
		"https://us05web.zoom.us/j/83136258594?pwd=lvaAdOGQl3oDOYbKRq9mQtkF4WD76j.1",
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
