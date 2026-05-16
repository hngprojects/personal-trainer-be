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
	slots, err := r.q.GetTrainersBookingSlots(ctx, trainerID)
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
