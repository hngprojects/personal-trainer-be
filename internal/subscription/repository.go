package subscription

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

var ErrNotFound = errors.New("not found")

type Repository interface {
	// plans
	GetPlanByType(ctx context.Context, planType string) (*db.SubscriptionPlan, error)

	// subscriptions
	getActiveSubscriptionForClient(ctx context.Context, clientID, trainerID string) (*db.Subscription, error)
	CreateSubscription(ctx context.Context, q *db.Queries, params db.CreateSubscriptionParams) (*db.Subscription, error)
	ActivateSubscription(ctx context.Context, q *db.Queries, id string) (*db.Subscription, error)
	ListSubscriptions(ctx context.Context, clientID string) ([]db.Subscription, error)
	GetSubscriptionByID(ctx context.Context, id string) (*db.Subscription, error)
	CancelSubscription(ctx context.Context, id string) (*db.Subscription, error)

	// payments
	GetPaymentByIdempotencyKey(ctx context.Context, key string) (*db.Payment, error)
	CreatePayment(ctx context.Context, q *db.Queries, params db.CreatePaymentParams) (*db.Payment, error)
	ConfirmPayment(ctx context.Context, q *db.Queries, id, providerTransactionID string) (*db.Payment, error)
	ListPayments(ctx context.Context, payerID string, limit, offset int32) ([]db.Payment, error)
	GetPaymentByID(ctx context.Context, id string) (*db.Payment, error)

	// wallets
	UpsertTrainerWallet(ctx context.Context, q *db.Queries, trainerID string, amount int64) (*db.TrainerWallet, error)
	CreateLedgerEntry(ctx context.Context, q *db.Queries, params db.CreateLedgerEntryParams) (*db.TrainerWalletLedger, error)
}

type postgresRepo struct {
	q  *db.Queries
	db *sql.DB
}

func NewPostgresRepo(q *db.Queries, db *sql.DB) Repository {
	return &postgresRepo{q: q, db: db}
}

func (r *postgresRepo) GetPlanByType(ctx context.Context, planType string) (*db.SubscriptionPlan, error) {
	return nil, nil
}

func (r *postgresRepo) getActiveSubscriptionForClient(ctx context.Context, clientID, trainerID string) (*db.Subscription, error) {
	return nil, nil
}

func (r *postgresRepo) CreateSubscription(ctx context.Context, q *db.Queries, params db.CreateSubscriptionParams) (*db.Subscription, error) {
	return nil, nil
}

func (r *postgresRepo) ActivateSubscription(ctx context.Context, q *db.Queries, id string) (*db.Subscription, error) {
	return nil, nil
}

func (r *postgresRepo) ListSubscriptions(ctx context.Context, clientID string) ([]db.Subscription, error) {
	id, err := uuid.Parse(clientID)
	if err != nil {
		return nil, err
	}

	subs, err := r.q.ListSubscriptions(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []db.Subscription{}, nil
		}
		return nil, err
	}

	return subs, nil
}

func (r *postgresRepo) GetSubscriptionByID(ctx context.Context, id string) (*db.Subscription, error) {
	subId, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}

	sub, err := r.q.GetSubscriptionByID(ctx, subId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &sub, nil
}

func (r *postgresRepo) CancelSubscription(ctx context.Context, id string) (*db.Subscription, error) {
	return nil, nil
}

func (r *postgresRepo) GetPaymentByIdempotencyKey(ctx context.Context, key string) (*db.Payment, error) {
	return nil, nil
}

func (r *postgresRepo) CreatePayment(ctx context.Context, q *db.Queries, params db.CreatePaymentParams) (*db.Payment, error) {
	return nil, nil
}

func (r *postgresRepo) ConfirmPayment(ctx context.Context, q *db.Queries, id, providerTransactionID string) (*db.Payment, error) {
	return nil, nil
}

func (r *postgresRepo) ListPayments(ctx context.Context, payerID string, limit, offset int32) ([]db.Payment, error) {
	return nil, nil
}

func (r *postgresRepo) GetPaymentByID(ctx context.Context, id string) (*db.Payment, error) {
	return nil, nil
}

func (r *postgresRepo) UpsertTrainerWallet(ctx context.Context, q *db.Queries, trainerID string, amount int64) (*db.TrainerWallet, error) {
	return nil, nil
}

func (r *postgresRepo) CreateLedgerEntry(ctx context.Context, q *db.Queries, params db.CreateLedgerEntryParams) (*db.TrainerWalletLedger, error) {
	return nil, nil
}
