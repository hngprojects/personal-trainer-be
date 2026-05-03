package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
)

type contextKey string

const UserIDKey contextKey = "userID"

func Auth(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				http.Error(w, `{"error":"invalid authorization header"}`, http.StatusUnauthorized)
				return
			}

			token := parts[1]

			var userID int64
			// var expiresAt string
			err := db.QueryRowContext(r.Context(),
			"SELECT user_id FROM sessions WHERE token = $1 AND expires_at > NOW()", token,
			).Scan(&userID)
			if err != nil {
				http.Error(w, `{"error":"invalid or expired session"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}