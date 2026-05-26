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
	ErrNotFound        = auth.ErrNotFound
	ErrTrainerNotFound = errors.New("trainer not found")
)

type Repository interface {
	GetBookingByID(ctx context.Context, id uuid.UUID) (db.Booking, error)
	CheckPaidBookingConflict(ctx context.Context, arg db.CheckPaidBookingConflictParams) (int64, error)
	ReschedulePaidBooking(ctx context.Context, arg db.ReschedulePaidBookingParams) (db.Booking, error)
	CreatePaidRescheduleHistory(ctx context.Context, arg db.CreatePaidRescheduleHistoryParams) error
	GetUserByID(ctx context.Context, id uuid.UUID) (db.User, error)
	GetTrainerByID(ctx context.Context, id uuid.UUID) (db.Trainer, error)
	FindBookingSlotByTrainerID(ctx context.Context, trainerID uuid.UUID) ([]db.GetTrainersBookingSlotsRow, error)
	CreateBooking(ctx context.Context, args db.CreateBookingParams) (*db.Booking, error)
	GetSubscriptionDetails(ctx context.Context, subID uuid.UUID) (db.Subscription, error)
	GetTrainerDetails(ctx context.Context, trainerID uuid.UUID) (db.GetTrainerUserDetailsRow, error)
	UpdateBookingZoom(ctx context.Context, arg db.UpdateBookingZoomParams) (db.Booking, error)
	CreateBookingSession(ctx context.Context, arg db.CreateBookingSessionParams) (db.BookingSession, error)
	// GetBookingSessionByBookingID is used by reschedule emails so they
	// can build the same universal-link "Join" URL the initial
	// confirmation used.
	GetBookingSessionByBookingID(ctx context.Context, bookingID uuid.UUID) (db.BookingSession, error)
}

type postgresRepo struct {
	q *db.Queries
}

func NewPostgresRepo(q *db.Queries) Repository {
	return &postgresRepo{q: q}
}

func (r *postgresRepo) FindBookingSlotByTrainerID(ctx context.Context, trainerID uuid.UUID) ([]db.GetTrainersBookingSlotsRow, error) {
	_, err := r.q.GetTrainerByID(ctx, trainerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTrainerNotFound
		}
		return nil, err
	}
	// sqlc inferred trainer_id as uuid.NullUUID for this query (defensive
	// default for untyped $1 params); wrap the non-null caller value so the
	// types line up. The booking_slots.trainer_id column is itself NOT NULL
	// at the schema level, so Valid is always true here.
	slots, err := r.q.GetTrainersBookingSlots(ctx, uuid.NullUUID{UUID: trainerID, Valid: true})
	if err != nil {
		return nil, err
	}
	return slots, nil
}

func (r *postgresRepo) CreateBooking(ctx context.Context, args db.CreateBookingParams) (*db.Booking, error) {
	booking, err := r.q.CreateBooking(ctx, args)
	if err != nil {
		return nil, err
	}
	return &booking, nil
}

func (r *postgresRepo) GetBookingByID(ctx context.Context, id uuid.UUID) (db.Booking, error) {
	return r.q.GetBookingByID(ctx, id)
}

func (r *postgresRepo) CheckPaidBookingConflict(ctx context.Context, arg db.CheckPaidBookingConflictParams) (int64, error) {
	return r.q.CheckPaidBookingConflict(ctx, arg)
}

func (r *postgresRepo) ReschedulePaidBooking(ctx context.Context, arg db.ReschedulePaidBookingParams) (db.Booking, error) {
	return r.q.ReschedulePaidBooking(ctx, arg)
}

func (r *postgresRepo) CreatePaidRescheduleHistory(ctx context.Context, arg db.CreatePaidRescheduleHistoryParams) error {
	return r.q.CreatePaidRescheduleHistory(ctx, arg)
}

func (r *postgresRepo) GetUserByID(ctx context.Context, id uuid.UUID) (db.User, error) {
	return r.q.GetUserByID(ctx, id)
}

func (r *postgresRepo) GetTrainerByID(ctx context.Context, id uuid.UUID) (db.Trainer, error) {
	return r.q.GetTrainerByID(ctx, id)
}

func (r *postgresRepo) GetTrainerDetails(ctx context.Context, trainerID uuid.UUID) (db.GetTrainerUserDetailsRow, error) {
	return r.q.GetTrainerUserDetails(ctx, trainerID)
}

func (r *postgresRepo) GetSubscriptionDetails(ctx context.Context, subID uuid.UUID) (db.Subscription, error) {
	return r.q.GetSubscriptionByID(ctx, subID)
}

func (r *postgresRepo) UpdateBookingZoom(ctx context.Context, arg db.UpdateBookingZoomParams) (db.Booking, error) {
	return r.q.UpdateBookingZoom(ctx, arg)
}

func (r *postgresRepo) CreateBookingSession(ctx context.Context, arg db.CreateBookingSessionParams) (db.BookingSession, error) {
	return r.q.CreateBookingSession(ctx, arg)
}

func (r *postgresRepo) GetBookingSessionByBookingID(ctx context.Context, bookingID uuid.UUID) (db.BookingSession, error) {
	return r.q.GetBookingSessionByBookingID(ctx, bookingID)
}
