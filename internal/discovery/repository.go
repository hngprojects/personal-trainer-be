package discovery

import (
	"context"
	"time"

	"github.com/google/uuid"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type Repository interface {
	CreateBooking(ctx context.Context, arg db.CreateDiscoveryBookingParams) (db.DiscoveryBooking, error)
	GetBookingByID(ctx context.Context, id uuid.UUID) (db.DiscoveryBooking, error)
	CheckSlotConflict(ctx context.Context, selectedDatetime time.Time) (int64, error)

	GetActiveSlots(ctx context.Context) ([]db.BookingSlot, error)
	GetSlotByID(ctx context.Context, id uuid.UUID) (db.BookingSlot, error)
	CreateSlot(ctx context.Context, arg db.CreateBookingSlotParams) (db.BookingSlot, error)
	UpdateSlot(ctx context.Context, arg db.UpdateBookingSlotParams) (db.BookingSlot, error)
	DeleteSlot(ctx context.Context, id uuid.UUID) error
}

type postgresRepo struct {
	q *db.Queries
}

func NewPostgresRepo(q *db.Queries) Repository {
	return &postgresRepo{q: q}
}

func (r *postgresRepo) CreateBooking(ctx context.Context, arg db.CreateDiscoveryBookingParams) (db.DiscoveryBooking, error) {
	return r.q.CreateDiscoveryBooking(ctx, arg)
}

func (r *postgresRepo) GetBookingByID(ctx context.Context, id uuid.UUID) (db.DiscoveryBooking, error) {
	return r.q.GetDiscoveryBookingByID(ctx, id)
}

func (r *postgresRepo) CheckSlotConflict(ctx context.Context, selectedDatetime time.Time) (int64, error) {
	return r.q.CheckSlotConflict(ctx, selectedDatetime)
}

func (r *postgresRepo) GetActiveSlots(ctx context.Context) ([]db.BookingSlot, error) {
	return r.q.GetActiveBookingSlots(ctx)
}

func (r *postgresRepo) GetSlotByID(ctx context.Context, id uuid.UUID) (db.BookingSlot, error) {
	return r.q.GetBookingSlotByID(ctx, id)
}

func (r *postgresRepo) CreateSlot(ctx context.Context, arg db.CreateBookingSlotParams) (db.BookingSlot, error) {
	return r.q.CreateBookingSlot(ctx, arg)
}

func (r *postgresRepo) UpdateSlot(ctx context.Context, arg db.UpdateBookingSlotParams) (db.BookingSlot, error) {
	return r.q.UpdateBookingSlot(ctx, arg)
}

func (r *postgresRepo) DeleteSlot(ctx context.Context, id uuid.UUID) error {
	return r.q.DeleteBookingSlot(ctx, id)
}
