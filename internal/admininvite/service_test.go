package admininvite_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/admininvite"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	errs "github.com/hngprojects/personal-trainer-be/pkg/errors"
)

// --- fakes -----------------------------------------------------------------

type fakeUsers struct {
	findUser *db.User
	findErr  error
}

func (f *fakeUsers) FindByEmail(_ context.Context, _ string) (*db.User, error) {
	return f.findUser, f.findErr
}
func (f *fakeUsers) Create(_ context.Context, email, name string) (*db.User, error) {
	return &db.User{ID: uuid.New(), Email: email, Name: name}, nil
}
func (f *fakeUsers) CreateLocal(_ context.Context, email, name, _ string) (*db.User, error) {
	return &db.User{ID: uuid.New(), Email: email, Name: name}, nil
}
func (f *fakeUsers) UpdateLastLogin(_ context.Context, _ uuid.UUID) error             { return nil }
func (f *fakeUsers) ListRoleNames(_ context.Context, _ uuid.UUID) ([]string, error)   { return nil, nil }
func (f *fakeUsers) AssignRole(_ context.Context, _ uuid.UUID, _ string) error        { return nil }
func (f *fakeUsers) WithTx(_ *sql.Tx) auth.UserRepository                             { return f }

type fakeRepo struct {
	invites      map[string]*db.AdminInvite // keyed by token_hash
	pending      bool
	pendingErr   error
	createErr    error
	capturedHash string // set by Create
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{invites: map[string]*db.AdminInvite{}}
}

func (f *fakeRepo) Create(_ context.Context, email, name, hash string, invitedBy uuid.UUID, expiresAt time.Time) (*db.AdminInvite, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	inv := &db.AdminInvite{
		ID: uuid.New(), Email: email, Name: name, TokenHash: hash,
		InvitedBy: invitedBy, ExpiresAt: expiresAt,
	}
	f.invites[hash] = inv
	f.capturedHash = hash
	return inv, nil
}

func (f *fakeRepo) FindByHash(_ context.Context, hash string) (*db.AdminInvite, error) {
	inv, ok := f.invites[hash]
	if !ok {
		return nil, errs.ErrNotFound
	}
	return inv, nil
}

func (f *fakeRepo) MarkAccepted(_ context.Context, id uuid.UUID) error {
	for _, inv := range f.invites {
		if inv.ID == id {
			inv.AcceptedAt = sql.NullTime{Time: time.Now(), Valid: true}
			return nil
		}
	}
	return errs.ErrNotFound
}

func (f *fakeRepo) HasPendingForEmail(_ context.Context, _ string) (bool, error) {
	return f.pending, f.pendingErr
}

func (f *fakeRepo) WithTx(_ *sql.Tx) admininvite.Repository { return f }

type fakeMailer struct {
	err     error
	to      string
	subject string
	body    string
}

func (m *fakeMailer) Send(to, subject, body string) error {
	if m.err != nil {
		return m.err
	}
	m.to, m.subject, m.body = to, subject, body
	return nil
}

func newSvc(repo admininvite.Repository, users auth.UserRepository, mailer *fakeMailer) *admininvite.Service {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// nil *sql.DB is fine for tests that don't reach BeginTx (Create, Validate,
	// Accept-failure paths).
	return admininvite.NewService(nil, repo, users, mailer, log, "http://test")
}

// extractTokenFromEmail pulls the raw token out of a sent invite body so we can
// drive Validate/Accept through the service's public surface (the hashing is
// unexported).
func extractTokenFromEmail(t *testing.T, body string) string {
	t.Helper()
	const marker = "?token="
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("no %q in email body: %q", marker, body)
	}
	rest := body[i+len(marker):]
	if end := strings.IndexAny(rest, "\n\r "); end >= 0 {
		return rest[:end]
	}
	return rest
}

// --- Create ---------------------------------------------------------------

func TestCreate_Success_StoresInviteAndSendsEmail(t *testing.T) {
	repo := newFakeRepo()
	users := &fakeUsers{findErr: errs.ErrNotFound}
	mailer := &fakeMailer{}
	s := newSvc(repo, users, mailer)

	if err := s.Create(context.Background(), uuid.New(), "new@x.com", "New"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if mailer.to != "new@x.com" {
		t.Errorf("email to %q, want new@x.com", mailer.to)
	}
	if !strings.Contains(mailer.body, "?token=") {
		t.Error("email body missing ?token= link")
	}
	if !strings.Contains(mailer.body, "Hello New") {
		t.Error("email body missing personalized greeting")
	}
	if len(repo.invites) != 1 {
		t.Errorf("expected 1 invite stored, got %d", len(repo.invites))
	}
	// The stored hash should not equal the raw token (defence-in-depth check).
	rawToken := extractTokenFromEmail(t, mailer.body)
	if _, exists := repo.invites[rawToken]; exists {
		t.Error("repo stored raw token instead of hash")
	}
}

func TestCreate_RejectsExistingUser(t *testing.T) {
	users := &fakeUsers{findUser: &db.User{ID: uuid.New(), Email: "exists@x.com"}}
	mailer := &fakeMailer{}
	s := newSvc(newFakeRepo(), users, mailer)

	err := s.Create(context.Background(), uuid.New(), "exists@x.com", "X")
	if !errors.Is(err, errs.ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
	if mailer.to != "" {
		t.Error("email sent for duplicate-user invite")
	}
}

func TestCreate_RejectsPendingInvite(t *testing.T) {
	repo := newFakeRepo()
	repo.pending = true
	users := &fakeUsers{findErr: errs.ErrNotFound}
	mailer := &fakeMailer{}
	s := newSvc(repo, users, mailer)

	err := s.Create(context.Background(), uuid.New(), "new@x.com", "New")
	if !errors.Is(err, errs.ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
	if mailer.to != "" {
		t.Error("email sent when pending invite already exists")
	}
}

func TestCreate_PropagatesUserLookupError(t *testing.T) {
	users := &fakeUsers{findErr: errors.New("db down")}
	s := newSvc(newFakeRepo(), users, &fakeMailer{})
	err := s.Create(context.Background(), uuid.New(), "new@x.com", "New")
	if err == nil || errors.Is(err, errs.ErrConflict) || errors.Is(err, errs.ErrNotFound) {
		t.Errorf("expected raw error, got %v", err)
	}
}

// --- Validate -------------------------------------------------------------

func TestValidate_RejectsUnknownToken(t *testing.T) {
	s := newSvc(newFakeRepo(), &fakeUsers{}, &fakeMailer{})
	_, err := s.Validate(context.Background(), "garbage-token")
	if !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestValidate_AcceptsLiveToken(t *testing.T) {
	repo := newFakeRepo()
	users := &fakeUsers{findErr: errs.ErrNotFound}
	mailer := &fakeMailer{}
	s := newSvc(repo, users, mailer)

	if err := s.Create(context.Background(), uuid.New(), "new@x.com", "New"); err != nil {
		t.Fatal(err)
	}
	token := extractTokenFromEmail(t, mailer.body)

	res, err := s.Validate(context.Background(), token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if res.Email != "new@x.com" || res.Name != "New" {
		t.Errorf("got email=%q name=%q", res.Email, res.Name)
	}
}

func TestValidate_RejectsExpiredInvite(t *testing.T) {
	repo := newFakeRepo()
	mailer := &fakeMailer{}
	s := newSvc(repo, &fakeUsers{findErr: errs.ErrNotFound}, mailer)

	if err := s.Create(context.Background(), uuid.New(), "new@x.com", "New"); err != nil {
		t.Fatal(err)
	}
	token := extractTokenFromEmail(t, mailer.body)
	repo.invites[repo.capturedHash].ExpiresAt = time.Now().Add(-time.Hour)

	_, err := s.Validate(context.Background(), token)
	if !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("expected ErrNotFound for expired, got %v", err)
	}
}

func TestValidate_RejectsAcceptedInvite(t *testing.T) {
	repo := newFakeRepo()
	mailer := &fakeMailer{}
	s := newSvc(repo, &fakeUsers{findErr: errs.ErrNotFound}, mailer)

	if err := s.Create(context.Background(), uuid.New(), "new@x.com", "New"); err != nil {
		t.Fatal(err)
	}
	token := extractTokenFromEmail(t, mailer.body)
	repo.invites[repo.capturedHash].AcceptedAt = sql.NullTime{Time: time.Now(), Valid: true}

	_, err := s.Validate(context.Background(), token)
	if !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("expected ErrNotFound for accepted, got %v", err)
	}
}

func TestValidate_RejectsRevokedInvite(t *testing.T) {
	repo := newFakeRepo()
	mailer := &fakeMailer{}
	s := newSvc(repo, &fakeUsers{findErr: errs.ErrNotFound}, mailer)

	if err := s.Create(context.Background(), uuid.New(), "new@x.com", "New"); err != nil {
		t.Fatal(err)
	}
	token := extractTokenFromEmail(t, mailer.body)
	repo.invites[repo.capturedHash].RevokedAt = sql.NullTime{Time: time.Now(), Valid: true}

	_, err := s.Validate(context.Background(), token)
	if !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("expected ErrNotFound for revoked, got %v", err)
	}
}

// --- Accept (failure paths only) -----------------------------------------
//
// The success path of Accept opens a real *sql.Tx, so it can't be unit-tested
// without sqlmock or a live Postgres. Cover that via integration tests.

func TestAccept_RejectsUnknownToken(t *testing.T) {
	s := newSvc(newFakeRepo(), &fakeUsers{}, &fakeMailer{})
	_, err := s.Accept(context.Background(), "garbage")
	if !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestAccept_RejectsExpiredInvite(t *testing.T) {
	repo := newFakeRepo()
	mailer := &fakeMailer{}
	s := newSvc(repo, &fakeUsers{findErr: errs.ErrNotFound}, mailer)

	if err := s.Create(context.Background(), uuid.New(), "new@x.com", "New"); err != nil {
		t.Fatal(err)
	}
	token := extractTokenFromEmail(t, mailer.body)
	repo.invites[repo.capturedHash].ExpiresAt = time.Now().Add(-time.Hour)

	_, err := s.Accept(context.Background(), token)
	if !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestAccept_RejectsAcceptedInvite(t *testing.T) {
	repo := newFakeRepo()
	mailer := &fakeMailer{}
	s := newSvc(repo, &fakeUsers{findErr: errs.ErrNotFound}, mailer)

	if err := s.Create(context.Background(), uuid.New(), "new@x.com", "New"); err != nil {
		t.Fatal(err)
	}
	token := extractTokenFromEmail(t, mailer.body)
	repo.invites[repo.capturedHash].AcceptedAt = sql.NullTime{Time: time.Now(), Valid: true}

	_, err := s.Accept(context.Background(), token)
	if !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
