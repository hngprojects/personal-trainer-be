package subscription

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/lib/pq"

	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")

type Repository interface {
	GetPlanByType(ctx context.Context, planType string) (*db.SubscriptionPlan, error)
	GetActiveSubscriptionForClient(ctx context.Context, clientID, trainerID uuid.UUID) (*db.Subscription, error)
	CreateSubscription(ctx context.Context, params db.CreateSubscriptionParams) (*db.Subscription, error)
	ListSubscriptions(ctx context.Context, clientID uuid.UUID) ([]db.Subscription, error)
	GetSubscriptionByID(ctx context.Context, id uuid.UUID) (*db.Subscription, error)
	CancelSubscription(ctx context.Context, id uuid.UUID) (*db.Subscription, error)

	GetPaymentByIdempotencyKey(ctx context.Context, key string) (*db.GetPaymentByIdempotencyKeyRow, error)
	CreatePayment(ctx context.Context, params db.CreatePaymentParams) (*db.CreatePaymentRow, error)
	ConfirmPayment(ctx context.Context, id uuid.UUID, providerTransactionID string) (*db.ConfirmPaymentRow, error) // reserved for Stripe webhook handler
	FailPayment(ctx context.Context, id uuid.UUID) error                                                          // reserved for Stripe webhook handler
	FlagPaymentForReconciliation(ctx context.Context, id uuid.UUID) error
	ListPayments(ctx context.Context, payerID uuid.UUID, limit, offset int32) ([]db.ListPaymentsRow, error)
	GetPaymentByID(ctx context.Context, id uuid.UUID) (*db.GetPaymentByIDRow, error)

	UpsertTrainerWallet(ctx context.Context, trainerID uuid.UUID, amount int64) (*db.TrainerWallet, error)
	CreateLedgerEntry(ctx context.Context, params db.CreateLedgerEntryParams) (*db.TrainerWalletLedger, error)

	GetTrainerByID(ctx context.Context, id uuid.UUID) (*db.Trainer, error)
	GetBookingByID(ctx context.Context, id uuid.UUID) (*db.Booking, error)
}

type postgresRepo struct {
	q *db.Queries
}

func NewPostgresRepo(q *db.Queries) Repository {
	return &postgresRepo{q: q}
}

func (r *postgresRepo) GetPlanByType(ctx context.Context, planType string) (*db.SubscriptionPlan, error) {
	plan, err := r.q.GetPlanByType(ctx, planType)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &plan, nil
}

func (r *postgresRepo) GetActiveSubscriptionForClient(ctx context.Context, clientID, trainerID uuid.UUID) (*db.Subscription, error) {
	sub, err := r.q.GetActiveSubscriptionForClient(ctx, db.GetActiveSubscriptionForClientParams{
		ClientID:  clientID,
		TrainerID: trainerID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &sub, nil
}

func (r *postgresRepo) CreateSubscription(ctx context.Context, params db.CreateSubscriptionParams) (*db.Subscription, error) {
	sub, err := r.q.CreateSubscription(ctx, params)
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrConflict
		}
		return nil, err
	}
	return &sub, nil
}

func (r *postgresRepo) ListSubscriptions(ctx context.Context, clientID uuid.UUID) ([]db.Subscription, error) {
	subs, err := r.q.ListSubscriptions(ctx, clientID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []db.Subscription{}, nil
		}
		return nil, err
	}
	if subs == nil {
		return []db.Subscription{}, nil
	}
	return subs, nil
}

func (r *postgresRepo) GetSubscriptionByID(ctx context.Context, id uuid.UUID) (*db.Subscription, error) {
	sub, err := r.q.GetSubscriptionByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &sub, nil
}

func (r *postgresRepo) CancelSubscription(ctx context.Context, id uuid.UUID) (*db.Subscription, error) {
	sub, err := r.q.CancelSubscription(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &sub, nil
}

func (r *postgresRepo) GetPaymentByIdempotencyKey(ctx context.Context, key string) (*db.GetPaymentByIdempotencyKeyRow, error) {
	row, err := r.q.GetPaymentByIdempotencyKey(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func (r *postgresRepo) CreatePayment(ctx context.Context, params db.CreatePaymentParams) (*db.CreatePaymentRow, error) {
	row, err := r.q.CreatePayment(ctx, params)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *postgresRepo) ConfirmPayment(ctx context.Context, id uuid.UUID, providerTransactionID string) (*db.ConfirmPaymentRow, error) {
	row, err := r.q.ConfirmPayment(ctx, db.ConfirmPaymentParams{
		ID:                    id,
		ProviderTransactionID: sql.NullString{String: providerTransactionID, Valid: true},
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

func (r *postgresRepo) FailPayment(ctx context.Context, id uuid.UUID) error {
	_, err := r.q.FailPayment(ctx, id)
	return err
}

func (r *postgresRepo) FlagPaymentForReconciliation(ctx context.Context, id uuid.UUID) error {
	return r.q.FlagPaymentForReconciliation(ctx, id)
}

func (r *postgresRepo) ListPayments(ctx context.Context, payerID uuid.UUID, limit, offset int32) ([]db.ListPaymentsRow, error) {
	rows, err := r.q.ListPayments(ctx, db.ListPaymentsParams{
		PayerID: payerID,
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []db.ListPaymentsRow{}, nil
		}
		return nil, err
	}
	if rows == nil {
		return []db.ListPaymentsRow{}, nil
	}
	return rows, nil
}

func (r *postgresRepo) GetPaymentByID(ctx context.Context, id uuid.UUID) (*db.GetPaymentByIDRow, error) {
	row, err := r.q.GetPaymentByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

func (r *postgresRepo) UpsertTrainerWallet(ctx context.Context, trainerID uuid.UUID, amount int64) (*db.TrainerWallet, error) {
	wallet, err := r.q.UpsertTrainerWallet(ctx, db.UpsertTrainerWalletParams{
		TrainerID: trainerID,
		Amount:    amount,
	})
	if err != nil {
		return nil, err
	}
	return &wallet, nil
}

func (r *postgresRepo) CreateLedgerEntry(ctx context.Context, params db.CreateLedgerEntryParams) (*db.TrainerWalletLedger, error) {
	entry, err := r.q.CreateLedgerEntry(ctx, params)
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

func (r *postgresRepo) GetTrainerByID(ctx context.Context, id uuid.UUID) (*db.Trainer, error) {
	trainer, err := r.q.GetTrainerByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &trainer, nil
}

func (r *postgresRepo) GetBookingByID(ctx context.Context, id uuid.UUID) (*db.Booking, error) {
	booking, err := r.q.GetBookingByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &booking, nil
}
