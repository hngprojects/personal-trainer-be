package waitlist

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/lib/pq"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrDuplicate = errors.New("duplicate entry")
)

// WaitlistRepository defines what the waitlist feature needs from the waitlist table.
type WaitlistRepository interface {
	AddEmail(ctx context.Context, email, phoneNumber, location, name string) error
	GetAll(ctx context.Context) ([]db.Waitlist, error)
	GetByEmail(ctx context.Context, email string) (*db.Waitlist, error)
}

// postgresWaitlistRepo implements WaitlistRepository using sqlc-generated queries.
type postgresWaitlistRepo struct {
	q *db.Queries
}

func NewPostgresWaitlistRepo(q *db.Queries) WaitlistRepository {
	return &postgresWaitlistRepo{q: q}
}

func (r *postgresWaitlistRepo) AddEmail(ctx context.Context, email, phoneNumber, location, name string) error {
	_, err := r.q.AddWaitlist(ctx, db.AddWaitlistParams{
		Email:       email,
		PhoneNumber: phoneNumber,
		Location:    location,
		Name:        name,
	})
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

func (r *postgresWaitlistRepo) GetAll(ctx context.Context) ([]db.Waitlist, error) {
	waitlists, err := r.q.GetWaitlist(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []db.Waitlist{}, nil
		}
		return nil, err
	}
	return waitlists, nil
}

func (r *postgresWaitlistRepo) GetByEmail(ctx context.Context, email string) (*db.Waitlist, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	waitlist, err := r.q.GetSingleWaitlist(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &waitlist, nil
}
