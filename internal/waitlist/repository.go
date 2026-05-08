package waitlist

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

var ErrNotFound = errors.New("not found")

// WaitlistRepository defines what the waitlist feature needs from the waitlist table.
type WaitlistRepository interface {
	AddEmail(ctx context.Context, email, feedback string) error
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

func (r *postgresWaitlistRepo) AddEmail(ctx context.Context, email, feedback string) error {
	_, err := r.q.AddWaitlist(ctx, db.AddWaitlistParams{
		Email:    email,
		Feedback: feedback,
	})
	if err != nil {
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
		// optionally map sql.ErrNoRows → ErrNotFound if not already handled in sqlc config
		return nil, ErrNotFound
	}

	return &waitlist, nil
}
