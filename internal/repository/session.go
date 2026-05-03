package repository

import (
	"context"

	"github.com/hngprojects/personal-trainer-be/internal/db"
	"github.com/hngprojects/personal-trainer-be/internal/models"
)

type SessionRepository struct {
	q *db.Queries
}

func NewSessionRepository(q *db.Queries) *SessionRepository {
	return &SessionRepository{q: q}
}

func (r *SessionRepository) Create(ctx context.Context, session *models.Session) error {
	row, err := r.q.CreateSession(ctx, db.CreateSessionParams{
		UserID:    session.UserID,
		Token:     session.Token,
		ExpiresAt: session.ExpiresAt,
	})
	if err != nil {
		return err
	}
	session.ID = row.ID
	session.CreatedAt = row.CreatedAt
	return nil
}

func (r *SessionRepository) FindByToken(ctx context.Context, token string) (*models.Session, error) {
	row, err := r.q.GetSessionByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	return &models.Session{
		ID:        row.ID,
		UserID:    row.UserID,
		Token:     row.Token,
		ExpiresAt: row.ExpiresAt,
		CreatedAt: row.CreatedAt,
	}, nil
}

func (r *SessionRepository) Delete(ctx context.Context, token string) error {
	return r.q.DeleteSession(ctx, token)
}
