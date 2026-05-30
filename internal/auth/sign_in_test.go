package auth_test

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/auth"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

// signInUser builds an active user row with the supplied bcrypt-hashed
// password. Tests pass plain text and the helper hashes it.
func signInUser(t *testing.T, email, plainPassword string) *db.User {
	t.Helper()
	return &db.User{
		ID:       uuid.New(),
		Email:    email,
		Name:     "Test User",
		IsActive: true,
		Password: sql.NullString{Valid: true, String: mustHashPassword(t, plainPassword)},
	}
}

// REGRESSION GUARD for the vuln deliberately shipped by PR #239
// ("feat(auth): email-only login returns tokens immediately"). That
// PR removed the bcrypt password check from SignIn, so anyone with a
// known email could log in as any user. A request without a password
// MUST now be rejected — never 200.
func TestSignIn_EmailOnlyMustNotIssueTokens(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	users := &fakeLocalUserRepo{findUser: signInUser(t, "victim@example.com", "real-password")}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, http.MethodPost, "/auth/login",
		`{"email":"victim@example.com"}`, h.SignIn)

	if w.Code == http.StatusOK {
		t.Fatalf("SECURITY REGRESSION: email-only login returned 200 — the PR #239 auth bypass is back. Body: %s", w.Body.String())
	}
	if w.Code != http.StatusBadRequest && w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 400 or 401 for email-only login, got %d: %s", w.Code, w.Body.String())
	}
	// Make sure NO tokens leaked into the response.
	if strings.Contains(w.Body.String(), "access_token") {
		t.Fatalf("response leaks access_token despite no password being verified: %s", w.Body.String())
	}
}

func TestSignIn_CorrectPasswordSucceeds(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	users := &fakeLocalUserRepo{findUser: signInUser(t, "jane@example.com", "S3cret!hunter2")}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, http.MethodPost, "/auth/login",
		`{"email":"jane@example.com","password":"S3cret!hunter2"}`, h.SignIn)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with correct password, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data object in response")
	}
	if at, _ := data["access_token"].(string); at == "" {
		t.Fatal("expected access_token in response")
	}
	if rt, _ := data["refresh_token"].(string); rt == "" {
		t.Fatal("expected refresh_token in response")
	}
}

func TestSignIn_WrongPasswordRejected(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	users := &fakeLocalUserRepo{findUser: signInUser(t, "jane@example.com", "the-real-password")}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, http.MethodPost, "/auth/login",
		`{"email":"jane@example.com","password":"WRONG"}`, h.SignIn)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong password, got %d: %s", w.Code, w.Body.String())
	}
	// Generic message — must NOT distinguish wrong-password from any
	// other credential failure (no email enumeration).
	if !strings.Contains(strings.ToLower(w.Body.String()), "invalid email or password") {
		t.Fatalf("expected generic 'invalid email or password' message, got: %s", w.Body.String())
	}
}

func TestSignIn_MissingPasswordField(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	users := &fakeLocalUserRepo{findUser: signInUser(t, "jane@example.com", "x")}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	// Body has email but no `password` key at all.
	w := doLocalRequest(t, http.MethodPost, "/auth/login",
		`{"email":"jane@example.com"}`, h.SignIn)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing password field, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSignIn_EmptyPasswordString(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	users := &fakeLocalUserRepo{findUser: signInUser(t, "jane@example.com", "x")}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, http.MethodPost, "/auth/login",
		`{"email":"jane@example.com","password":""}`, h.SignIn)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty password string, got %d: %s", w.Code, w.Body.String())
	}
}

// OAuth-only accounts (Google sign-up) have user.Password.Valid =
// false. They MUST NOT be able to log in via this endpoint regardless
// of what password they submit. Same generic 401 — no enumeration.
func TestSignIn_OAuthOnlyAccountRejected(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	oauthUser := &db.User{
		ID:       uuid.New(),
		Email:    "google-user@example.com",
		IsActive: true,
		Password: sql.NullString{Valid: false}, // never set
	}
	users := &fakeLocalUserRepo{findUser: oauthUser}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, http.MethodPost, "/auth/login",
		`{"email":"google-user@example.com","password":"anything"}`, h.SignIn)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for OAuth-only account, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(strings.ToLower(w.Body.String()), "invalid email or password") {
		t.Fatalf("expected generic message, got: %s", w.Body.String())
	}
}

func TestSignIn_UnknownEmailRejected(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	users := &fakeLocalUserRepo{findErr: auth.ErrNotFound}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, http.MethodPost, "/auth/login",
		`{"email":"nobody@example.com","password":"whatever"}`, h.SignIn)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unknown email, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(strings.ToLower(w.Body.String()), "invalid email or password") {
		t.Fatalf("expected generic message (no email enumeration), got: %s", w.Body.String())
	}
}

func TestSignIn_InactiveUserRejected(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	u := signInUser(t, "deactivated@example.com", "real-password")
	u.IsActive = false
	users := &fakeLocalUserRepo{findUser: u}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, http.MethodPost, "/auth/login",
		`{"email":"deactivated@example.com","password":"real-password"}`, h.SignIn)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for inactive user, got %d: %s", w.Code, w.Body.String())
	}
	// Inactive must NOT return a distinct message — same generic
	// 'invalid email or password' so an attacker can't tell that the
	// account exists but was deactivated.
	if !strings.Contains(strings.ToLower(w.Body.String()), "invalid email or password") {
		t.Fatalf("inactive must return same generic message as wrong password (no enumeration), got: %s", w.Body.String())
	}
}

// bcrypt only hashes the first 72 bytes; a longer password could
// authenticate against any hash sharing the same prefix. CheckPassword
// guards via ErrPasswordTooLong. Make sure SignIn returns 401, not 200,
// on a >72-byte input.
func TestSignIn_OverLongPasswordRejected(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	users := &fakeLocalUserRepo{findUser: signInUser(t, "jane@example.com", "real")}
	h := newLocalTestHandler(t, users, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	// 80-byte password; CheckPassword rejects anything > 72 bytes
	// before even invoking bcrypt.
	longPassword := strings.Repeat("a", 80)
	body := `{"email":"jane@example.com","password":"` + longPassword + `"}`

	w := doLocalRequest(t, http.MethodPost, "/auth/login", body, h.SignIn)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for >72-byte password, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSignIn_MalformedJSONRejected(t *testing.T) {
	h := newLocalTestHandler(t, &fakeLocalUserRepo{}, &fakeLocalSessionRepo{}, &fakeCodeRepo{}, &fakeLocalAuthRepo{}, &fakeMailer{})

	w := doLocalRequest(t, http.MethodPost, "/auth/login", `not json`, h.SignIn)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d: %s", w.Code, w.Body.String())
	}
}
