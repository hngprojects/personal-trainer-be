package bookings

import (
	"context"

	"github.com/google/uuid"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type Repository interface {
	GetBookingByID(ctx context.Context, id uuid.UUID) (db.Booking, error)
	CheckPaidBookingConflict(ctx context.Context, arg db.CheckPaidBookingConflictParams) (int64, error)
	ReschedulePaidBooking(ctx context.Context, arg db.ReschedulePaidBookingParams) (db.Booking, error)
	CreatePaidRescheduleHistory(ctx context.Context, arg db.CreatePaidRescheduleHistoryParams) error
	GetUserByID(ctx context.Context, id uuid.UUID) (db.User, error)
	GetTrainerByID(ctx context.Context, id uuid.UUID) (db.Trainer, error)
}

type postgresRepo struct {
	q *db.Queries
}

func NewPostgresRepo(q *db.Queries) Repository {
	return &postgresRepo{q: q}
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
