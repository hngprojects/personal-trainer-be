package bookings

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type BookingSlotService interface {
	GetTrainersBookingSlots(ctx context.Context, trainerId uuid.UUID) ([]db.GetTrainersBookingSlotsRow, error)
}
type BookingService interface {
	CreateBooking(ctx context.Context, args db.CreateBookingParams) (*db.Booking, error)
}

type bookingService struct {
	repo BookingsRepository
	log  *slog.Logger
}

func NewBookingSlotService(repo BookingsRepository, log *slog.Logger) BookingSlotService {
	return &bookingService{
		repo: repo,
		log:  log,
	}
}

func NewBookingService(repo BookingsRepository, log *slog.Logger) BookingService {
	return &bookingService{
		repo: repo,
		log:  log,
	}
}

func (s *bookingService) GetTrainersBookingSlots(ctx context.Context, trainerId uuid.UUID) ([]db.GetTrainersBookingSlotsRow, error) {
	slots, err := s.repo.FindBookingSlotByTrainerID(ctx, trainerId)
	if err != nil {
		return nil, err
	}
	return slots, nil
}

func (s *bookingService) CreateBooking(ctx context.Context, args db.CreateBookingParams) (*db.Booking, error) {
	booking, err := s.repo.CreateBooking(ctx, args)
	if err != nil {
		return nil, err
	}
	return booking, nil
}
