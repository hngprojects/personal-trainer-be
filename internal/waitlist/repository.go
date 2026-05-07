package waitlist

import (
	"context"
	"database/sql"
	"errors"

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
	// Note: You may need to create a GetWaitlistByEmail query in waitlist.sql
	// For now, this is a placeholder; implement based on your needs
	waitlists, err := r.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	for i := range waitlists {
		if waitlists[i].Email == email {
			return &waitlists[i], nil
		}
	}

	return nil, ErrNotFound
}
