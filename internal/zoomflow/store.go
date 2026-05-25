// Package zoomflow wires together everything the booking + join paths
// need to talk to Zoom in one of two modes:
//
//   - org mode: meetings created via the server-to-server account.
//     Existing behaviour, no per-user state.
//   - trainer mode: meetings created via the trainer's connected
//     OAuth grant. Tokens live encrypted in user_zoom_credentials;
//     this package handles loading, decrypting, refreshing, and
//     re-encrypting on write.
//
// Kept out of pkg/zoom on purpose: pkg/zoom holds the wire-level Zoom
// API clients with no opinion on storage or feature flags. zoomflow
// holds the application-level orchestration that needs db, config,
// and crypto.
package zoomflow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/cryptoutil"
	"github.com/hngprojects/personal-trainer-be/pkg/zoom"
)

// ErrNotConnected is returned by Store.LoadFreshToken when the user
// has no row in user_zoom_credentials. Callers (the selector +
// /zoom/status handler) check for this to distinguish "they haven't
// connected" from real errors like a revoked grant or a Zoom outage.
var ErrNotConnected = errors.New("zoomflow: user has not connected zoom")

// CredentialStore encapsulates the encrypt-decrypt-refresh dance for
// per-user OAuth tokens. Single entry point so we can't accidentally
// build a code path that reads tokens without going through refresh,
// or writes tokens without encrypting.
type CredentialStore struct {
	q       *db.Queries
	rawDB   *sql.DB
	enc     *cryptoutil.AESGCM
	oauth   *zoom.OAuthClient
	log     *slog.Logger
	// refreshMu serialises refresh attempts per-user so two concurrent
	// booking flows for the same trainer don't both try to swap the
	// refresh token (Zoom invalidates the old one on first success;
	// the loser of the race would persist garbage). Map of per-user
	// mutexes; the outer mu protects insertion into the map.
	mu        sync.Mutex
	refreshMu map[uuid.UUID]*sync.Mutex
}

func NewCredentialStore(q *db.Queries, rawDB *sql.DB, enc *cryptoutil.AESGCM, oauth *zoom.OAuthClient, log *slog.Logger) *CredentialStore {
	return &CredentialStore{
		q:         q,
		rawDB:     rawDB,
		enc:       enc,
		oauth:     oauth,
		log:       log,
		refreshMu: make(map[uuid.UUID]*sync.Mutex),
	}
}

// Status reports whether the user has a stored grant + when it
// expires. Cheap; doesn't trigger a refresh. Used by the
// /trainers/me/zoom/status handler.
func (s *CredentialStore) Status(ctx context.Context, userID uuid.UUID) (connected bool, email string, expiresAt time.Time, err error) {
	row, err := s.q.GetUserZoomCredentials(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, "", time.Time{}, nil
		}
		return false, "", time.Time{}, err
	}
	em := ""
	if row.ZoomEmail.Valid {
		em = row.ZoomEmail.String
	}
	return true, em, row.AccessTokenExpiresAt, nil
}

// PersistFromExchange writes a brand-new token pair from the OAuth
// callback. Used once per Connect flow.
func (s *CredentialStore) PersistFromExchange(ctx context.Context, userID uuid.UUID, tokens *zoom.TokenResponse, profile *zoom.UserProfile) error {
	encAccess, err := s.enc.Encrypt([]byte(tokens.AccessToken))
	if err != nil {
		return fmt.Errorf("encrypt access token: %w", err)
	}
	encRefresh, err := s.enc.Encrypt([]byte(tokens.RefreshToken))
	if err != nil {
		return fmt.Errorf("encrypt refresh token: %w", err)
	}
	_, err = s.q.UpsertUserZoomCredentials(ctx, db.UpsertUserZoomCredentialsParams{
		UserID:               userID,
		AccessTokenEnc:       encAccess,
		RefreshTokenEnc:      encRefresh,
		AccessTokenExpiresAt: tokens.ExpiresAt,
		Scope:                tokens.Scope,
		ZoomUserID:           profile.ID,
		ZoomAccountID:        profile.AccountID,
		ZoomEmail:            profile.Email,
	})
	return err
}

// Delete removes the user's stored credentials. Idempotent — returns
// (false, nil) if there was nothing to delete so the handler can pick
// between 204 and 404 itself.
func (s *CredentialStore) Delete(ctx context.Context, userID uuid.UUID) (existed bool, err error) {
	rows, err := s.q.DeleteUserZoomCredentials(ctx, userID)
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

// LoadFreshToken returns a usable access token for the user, refreshing
// transparently if the stored one is expired. Returns ErrNotConnected
// when no row exists.
//
// Serialises per-user refreshes so two concurrent callers can't both
// burn the rolling refresh token. The lock is released as soon as we
// have a fresh token; downstream API calls happen unlocked.
func (s *CredentialStore) LoadFreshToken(ctx context.Context, userID uuid.UUID) (string, error) {
	row, err := s.q.GetUserZoomCredentials(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotConnected
		}
		return "", fmt.Errorf("load credentials: %w", err)
	}

	// Fast path: stored token still valid (with a small skew buffer
	// already baked into ExpiresAt by oauth.go — see the comment
	// there about why we subtract 60s before persisting).
	if time.Now().Before(row.AccessTokenExpiresAt) {
		access, err := s.enc.Decrypt(row.AccessTokenEnc)
		if err != nil {
			return "", fmt.Errorf("decrypt access token: %w", err)
		}
		return string(access), nil
	}

	// Slow path: refresh required. Serialise per-user.
	lock := s.userMu(userID)
	lock.Lock()
	defer lock.Unlock()

	// Re-read under the lock — another goroutine may have refreshed
	// while we were waiting.
	row, err = s.q.GetUserZoomCredentials(ctx, userID)
	if err != nil {
		return "", err
	}
	if time.Now().Before(row.AccessTokenExpiresAt) {
		access, err := s.enc.Decrypt(row.AccessTokenEnc)
		if err != nil {
			return "", err
		}
		return string(access), nil
	}

	refreshBytes, err := s.enc.Decrypt(row.RefreshTokenEnc)
	if err != nil {
		return "", fmt.Errorf("decrypt refresh token: %w", err)
	}

	fresh, refreshErr := s.oauth.Refresh(ctx, string(refreshBytes))
	if refreshErr != nil {
		_ = s.q.RecordUserZoomFailure(ctx, db.RecordUserZoomFailureParams{
			UserID: userID,
			Reason: refreshErr.Error(),
		})
		s.log.Warn("zoomflow: refresh failed", "user_id", userID, "err", refreshErr)
		return "", fmt.Errorf("refresh zoom token: %w", refreshErr)
	}

	encAccess, err := s.enc.Encrypt([]byte(fresh.AccessToken))
	if err != nil {
		return "", err
	}
	encRefresh, err := s.enc.Encrypt([]byte(fresh.RefreshToken))
	if err != nil {
		return "", err
	}
	if _, err := s.q.UpdateUserZoomTokens(ctx, db.UpdateUserZoomTokensParams{
		UserID:               userID,
		AccessTokenEnc:       encAccess,
		RefreshTokenEnc:      encRefresh,
		AccessTokenExpiresAt: fresh.ExpiresAt,
		Scope:                fresh.Scope,
	}); err != nil {
		// Worst-case: we got a new token pair from Zoom but couldn't
		// persist. Returning the new access token risks the caller
		// using it on a request whose response we can't audit; safer
		// to fail loud and let the next request re-refresh (which
		// will fail because the OLD refresh token is now invalid —
		// trainer has to reconnect). Surface clearly.
		s.log.Error("zoomflow: token rotated upstream but DB update failed; trainer will need to reconnect",
			"user_id", userID, "err", err)
		return "", fmt.Errorf("persist refreshed tokens: %w", err)
	}
	return fresh.AccessToken, nil
}

// tokenSourceForUser bundles the userID + store into a
// zoom.TokenSource the UserProvider can hold without knowing about
// the store directly.
type tokenSourceForUser struct {
	userID uuid.UUID
	store  *CredentialStore
}

func (t *tokenSourceForUser) AccessToken(ctx context.Context) (string, error) {
	return t.store.LoadFreshToken(ctx, t.userID)
}

// NewUserTokenSource is the bridge between CredentialStore and the
// pkg/zoom UserProvider — gives the provider a TokenSource that
// transparently refreshes from the store.
func (s *CredentialStore) NewUserTokenSource(userID uuid.UUID) zoom.TokenSource {
	return &tokenSourceForUser{userID: userID, store: s}
}

func (s *CredentialStore) userMu(userID uuid.UUID) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.refreshMu[userID]
	if !ok {
		m = &sync.Mutex{}
		s.refreshMu[userID] = m
	}
	return m
}
