package admininvite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
	errs "github.com/hngprojects/personal-trainer-be/pkg/errors"
)

const (
	InviteTTL    = 72 * time.Hour
	GrantedRole  = "admin"
	tokenByteLen = 32
)

type Service struct {
	db          *sql.DB
	repo        Repository
	users       auth.UserRepository
	mailer      email.Mailer
	log         *slog.Logger
	frontendURL string
}

func NewService(db *sql.DB, repo Repository, users auth.UserRepository,
	mailer email.Mailer, log *slog.Logger, frontendURL string) *Service {
	return &Service{db: db, repo: repo, users: users, mailer: mailer,
		log: log, frontendURL: frontendURL}
}

// Create issues a new admin invite. The raw token only ever leaves the server
// inside the email body; the SHA-256 hash is what gets stored.
func (s *Service) Create(ctx context.Context, invitedBy uuid.UUID, inviteeEmail, name string) error {
	// Reject if the email is already a registered user.
	if _, err := s.users.FindByEmail(ctx, inviteeEmail); err == nil {
		return errs.ErrConflict
	} else if !errors.Is(err, errs.ErrNotFound) {
		return err
	}

	// Reject if there's already a pending unexpired invite for this email.
	if has, err := s.repo.HasPendingForEmail(ctx, inviteeEmail); err != nil {
		return err
	} else if has {
		return errs.ErrConflict
	}

	raw := make([]byte, tokenByteLen)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("generate token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := hashToken(token)

	if _, err := s.repo.Create(ctx, inviteeEmail, name, hash, invitedBy,
		time.Now().Add(InviteTTL)); err != nil {
		return err
	}

	link := fmt.Sprintf("%s/admin/accept?token=%s", s.frontendURL, token)
	body := fmt.Sprintf(
		"Hello %s,\n\nYou've been invited as an admin.\nAccept here: %s\n\nThis link expires in 72 hours.",
		name, link,
	)
	if err := s.mailer.Send(inviteeEmail, "You're invited as an admin", body); err != nil {
		s.log.Error("send invite email failed", "err", err, "email", inviteeEmail)
		return err
	}
	s.log.Info("admin invite sent", "email", inviteeEmail, "invited_by", invitedBy)
	return nil
}

// ValidateResult is what the public GET endpoint returns when a token is valid.
type ValidateResult struct {
	Email string
	Name  string
}

// Validate confirms a token is live and returns the invitee's email + name
// so the frontend can render the accept page.
func (s *Service) Validate(ctx context.Context, rawToken string) (*ValidateResult, error) {
	inv, err := s.lookup(ctx, rawToken)
	if err != nil {
		return nil, err
	}
	return &ValidateResult{Email: inv.Email, Name: inv.Name}, nil
}

// AcceptResult includes the generated password — it is the ONLY time the
// password leaves the server. Callers must never log this struct.
type AcceptResult struct {
	UserID            uuid.UUID
	Email             string
	Name              string
	GeneratedPassword string
	AccessToken       string
	RefreshToken      string
}

// Accept consumes a valid invite. In one transaction it: creates the user,
// assigns the admin role, and marks the invite accepted. Then it issues JWTs
// and returns the generated password for one-time display.
func (s *Service) Accept(ctx context.Context, rawToken string) (*AcceptResult, error) {
	inv, err := s.lookup(ctx, rawToken)
	if err != nil {
		return nil, err
	}

	// Generate + hash the password BEFORE opening the transaction — bcrypt is
	// slow (~100ms) and we don't want to hold a tx open during hashing.
	pwd, err := auth.GenerateRandomPassword(16)
	if err != nil {
		return nil, fmt.Errorf("generate password: %w", err)
	}
	hashed, err := auth.HashPassword(pwd)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	txUsers := s.users.WithTx(tx)
	txInvites := s.repo.WithTx(tx)

	user, err := txUsers.CreateLocal(ctx, inv.Email, inv.Name, hashed)
	if err != nil {
		return nil, err
	}
	if err := txUsers.AssignRole(ctx, user.ID, GrantedRole); err != nil {
		return nil, err
	}
	if err := txInvites.MarkAccepted(ctx, inv.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	access, err := auth.GenerateJWTToken(user.ID.String(), auth.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("issue access token: %w", err)
	}
	refresh, err := auth.GenerateJWTToken(user.ID.String(), auth.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("issue refresh token: %w", err)
	}

	s.log.Info("admin invite accepted", "user_id", user.ID, "email", user.Email)

	return &AcceptResult{
		UserID:            user.ID,
		Email:             user.Email,
		Name:              user.Name,
		GeneratedPassword: pwd,
		AccessToken:       access,
		RefreshToken:      refresh,
	}, nil
}

// lookup returns the invite if it exists and is still valid (not accepted,
// not revoked, not expired). All failure modes collapse to ErrNotFound so
// callers can't distinguish "wrong token" from "expired token" — enumeration
// resistance.
func (s *Service) lookup(ctx context.Context, rawToken string) (*db.AdminInvite, error) {
	inv, err := s.repo.FindByHash(ctx, hashToken(rawToken))
	if err != nil {
		return nil, errs.ErrNotFound
	}
	if inv.AcceptedAt.Valid || inv.RevokedAt.Valid || time.Now().After(inv.ExpiresAt) {
		return nil, errs.ErrNotFound
	}
	return inv, nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
