package bookings

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

var (
	activeSubscriptionStatus = "active"
	ErrNotFound              = auth.ErrNotFound
	ErrTrainerNotFound       = errors.New("trainer not found")
)

type BookingsRepository interface {
	FindBookingSlotByTrainerID(ctx context.Context, trainerID uuid.UUID) ([]db.GetTrainersBookingSlotsRow, error)
	CreateBooking(ctx context.Context, args db.CreateBookingParams) (*db.Booking, error)
	CheckSubscription(ctx context.Context, subscriptionID uuid.UUID) (db.GetSubscriptionRow, error)
}

type bookingRepo struct {
	q *db.Queries
}

func NewPostgresBookingRepository(q *db.Queries) BookingsRepository {
	return &bookingRepo{q: q}
}

func (r *bookingRepo) FindBookingSlotByTrainerID(ctx context.Context, trainerID uuid.UUID) ([]db.GetTrainersBookingSlotsRow, error) {
	_, err := r.q.GetTrainerByID(ctx, trainerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTrainerNotFound
		}
		return nil, err
	}
	slots, err := r.q.GetTrainersBookingSlots(ctx, trainerID)
	if err != nil {
		return nil, err
	}
	return slots, nil
}

func (r *bookingRepo) CreateBooking(ctx context.Context, args db.CreateBookingParams) (*db.Booking, error) {
	booking, err := r.q.CreateBooking(ctx, args)
	if err != nil {
		return nil, err
	}
	return &booking, nil
}

func (r *bookingRepo) CheckSubscription(ctx context.Context, subscriptionID uuid.UUID) (db.GetSubscriptionRow, error) {
	subscription, err := r.q.GetSubscription(ctx, subscriptionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.GetSubscriptionRow{}, ErrNotFound
		}
		return db.GetSubscriptionRow{}, err
	}
	return subscription, nil
}
