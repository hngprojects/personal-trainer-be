package auth_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

// userTypeCase is the table row for the role-mapping integration tests.
// Captures the bug repro: previously every login surfaced user_type
// "client" regardless of users.role.
type userTypeCase struct {
	name    string
	dbRole  string
	wantOut string
}

var userTypeCases = []userTypeCase{
	{"client", "client", "client"},
	{"trainer", "trainer", "trainer"},
	{"admin", "admin", "admin"},
	// super_admin collapses to admin on login responses — FE doesn't need
	// to distinguish at sign-in (the SuperAdminOnly middleware gates the
	// only routes where it matters).
	{"super_admin collapses to admin", "super_admin", "admin"},
	// Unknown / empty fall back to client so a brand-new role can't 500
	// the login path before the enum is updated.
	{"empty role -> client", "", "client"},
	{"unknown role -> client", "operator", "client"},
}

// TestSignIn_UserTypeMatchesRole exercises the regression directly:
// POST /auth/login must return data.user.user_type reflecting the
// user's actual users.role, not the hardcoded "client" string the
// old handler used.
func TestSignIn_UserTypeMatchesRole(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	for _, tc := range userTypeCases {
		t.Run(tc.name, func(t *testing.T) {
			pwHash, err := bcrypt.GenerateFromPassword([]byte("StrongP@ss123"), bcrypt.MinCost)
			if err != nil {
				t.Fatalf("bcrypt: %v", err)
			}
			users := &fakeLocalUserRepo{
				findUser: &db.User{
					ID:           uuid.New(),
					Email:        "u@example.com",
					Name:         "Test",
					Password:     sql.NullString{String: string(pwHash), Valid: true},
					AuthProvider: "local",
					IsActive:     true,
					Role:         tc.dbRole,
				},
			}

			h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

			w := doLocalRequest(t, http.MethodPost, "/auth/login",
				`{"email":"u@example.com","password":"StrongP@ss123"}`, h.SignIn)

			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			got := userTypeFrom(t, w.Body.Bytes())
			if got != tc.wantOut {
				t.Errorf("user_type: got %q, want %q", got, tc.wantOut)
			}
		})
	}
}

// TestVerifyEmail_UserTypeMatchesRole pins the same fix on the
// verify-email path, which doubles as the first login after registration.
func TestVerifyEmail_UserTypeMatchesRole(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	for _, tc := range userTypeCases {
		t.Run(tc.name, func(t *testing.T) {
			authRepo := &fakeLocalAuthRepo{
				user: &db.User{
					ID:           uuid.New(),
					Email:        "v@example.com",
					Name:         "Test",
					AuthProvider: "local",
					IsActive:     true,
					Role:         tc.dbRole,
				},
			}
			h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, authRepo, &fakeMailer{})

			w := doLocalRequest(t, http.MethodPost, "/auth/verify-email",
				`{"email":"v@example.com","code":"123456"}`, h.VerifyEmail)

			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			got := userTypeFrom(t, w.Body.Bytes())
			if got != tc.wantOut {
				t.Errorf("user_type: got %q, want %q", got, tc.wantOut)
			}
		})
	}
}

// userTypeFrom pulls data.user.user_type out of an auth response.
// Kept here (not promoted to local_test.go) because it's specific to
// these mapping tests and isn't useful elsewhere.
func userTypeFrom(t *testing.T, body []byte) string {
	t.Helper()
	var resp struct {
		Data struct {
			User struct {
				UserType string `json:"user_type"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, string(body))
	}
	return resp.Data.User.UserType
}
