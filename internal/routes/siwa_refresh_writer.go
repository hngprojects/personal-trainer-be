package routes

import (
	"context"
	"database/sql"

	"github.com/google/uuid"

	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

// siwaRefreshTokenWriter adapts the sqlc-generated query
// SetUserAppleRefreshToken to the SIWARefreshTokenWriter interface the
// auth package expects. Tiny adapter so internal/auth doesn't have to
// import sqlc-generated types directly.
type siwaRefreshTokenWriter struct {
	q *db.Queries
}

func (w *siwaRefreshTokenWriter) SetUserAppleRefreshToken(ctx context.Context, userID uuid.UUID, encryptedToken string) error {
	return w.q.SetUserAppleRefreshToken(ctx, db.SetUserAppleRefreshTokenParams{
		ID:                   userID,
		RefreshTokenEnc:      sql.NullString{String: encryptedToken, Valid: encryptedToken != ""},
	})
}
