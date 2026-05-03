package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const UserIDKey contextKey = "userID"

type Claims struct {
	UserID int64 `json:"user_id"`
	jwt.RegisteredClaims
}

func Auth(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeUnauthorized(w, "missing authorization header")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				writeUnauthorized(w, "invalid authorization header")
				return
			}

			tokenString := parts[1]

			// Step 1 — validate JWT signature
			// // if team confirms plain session tokens, remove this block
			// secret := []byte(os.Getenv("JWT_SECRET"))
			// token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
			// 	if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			// 		return nil, jwt.ErrSignatureInvalid
			// 	}
			// 	return secret, nil
			// })
			// if err != nil || !token.Valid {
			// 	writeUnauthorized(w, "invalid or expired token")
			// 	return
			// }

			// Step 2 — check session exists and not expired in DB
			var userID int64
			err := db.QueryRowContext(r.Context(),
				"SELECT user_id FROM sessions WHERE token = $1 AND expires_at > NOW()",
				tokenString,
			).Scan(&userID)
			if err != nil {
				writeUnauthorized(w, "session not found or expired")
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":{"code":"UNAUTHORIZED","message":"` + message + `"}}`))
}