package zoom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// TokenSource is whatever the caller knows how to do to produce a
// fresh access token for the request. The selector layer (in routes/)
// wires this up by loading the encrypted refresh token from the DB,
// decrypting, refreshing via OAuthClient if expired, and re-encrypting
// the new pair on its way back. The provider doesn't see any of that
// — it just asks for a token and gets one.
//
// Returning a real Go error (vs returning "" with no error) keeps the
// caller's error-classification simple: any returned error short-
// circuits the meeting create with a clean log line.
type TokenSource interface {
	AccessToken(ctx context.Context) (string, error)
}

// ErrNoUserConnection is returned by TokenSource implementations when
// the user simply hasn't connected their Zoom account yet. The
// selector layer special-cases this to fall back to the org provider
// rather than 5xx-ing the booking. Any OTHER error is treated as a
// real failure (revoked grant, Zoom outage, etc.) and propagated.
var ErrNoUserConnection = errors.New("zoom: user has no connected zoom account")

// UserProvider implements meeting.Provider against a per-user OAuth
// grant. Mirrors Client (the org-credentials provider) but takes a
// per-request TokenSource instead of long-lived account creds.
//
// Lifetime is per-request: routes/ constructs one of these for each
// CreateMeeting / DeleteMeeting call after loading the trainer's
// credentials. The constructor is intentionally tiny so the alloc
// cost on the hot path is negligible.
type UserProvider struct {
	tokens     TokenSource
	httpClient *http.Client
}

func NewUserProvider(ts TokenSource) *UserProvider {
	return &UserProvider{
		tokens:     ts,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// IsConfigured returns true when a token source is wired. The selector
// uses this only as a defensive guard; real configuration checks
// happen earlier when deciding which provider to construct.
func (u *UserProvider) IsConfigured() bool { return u != nil && u.tokens != nil }

// CreateMeeting mirrors Client.CreateMeeting but uses /users/me with
// the per-user token, so the meeting is owned by THAT user's Zoom
// account (visible in their dashboard, recordings under their plan,
// etc.).
func (u *UserProvider) CreateMeeting(ctx context.Context, topic string, startTime time.Time, durationMinutes int) (joinURL, meetingID string, err error) {
	if u == nil || u.tokens == nil {
		return "", "", ErrNoUserConnection
	}
	token, err := u.tokens.AccessToken(ctx)
	if err != nil {
		return "", "", err
	}
	body := map[string]interface{}{
		"topic":      topic,
		"type":       2,
		"start_time": startTime.UTC().Format("2006-01-02T15:04:05Z"),
		"duration":   durationMinutes,
		"settings": map[string]interface{}{
			"join_before_host": false, // host = the trainer; client waits
			"waiting_room":     false,
		},
	}
	b, err := json.Marshal(body)
	if err != nil {
		return "", "", fmt.Errorf("zoom user: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.zoom.us/v2/users/me/meetings", bytes.NewReader(b))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("zoom user: create meeting: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("zoom user: create meeting failed (%d): %s", resp.StatusCode, string(raw))
	}
	var m struct {
		ID      int64  `json:"id"`
		JoinURL string `json:"join_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return "", "", fmt.Errorf("zoom user: decode response: %w", err)
	}
	// Defensive: Zoom is supposed to return both id and join_url on a
	// 2xx, but bare 200s with an empty body would silently persist a
	// useless meeting record. Surface as an error so the booking flow
	// can refuse the slot rather than confirming a no-op call.
	if m.ID == 0 || m.JoinURL == "" {
		return "", "", fmt.Errorf("zoom user: create meeting returned empty id or join_url")
	}
	return m.JoinURL, fmt.Sprintf("%d", m.ID), nil
}

func (u *UserProvider) DeleteMeeting(ctx context.Context, meetingID string) error {
	if u == nil || u.tokens == nil {
		return ErrNoUserConnection
	}
	token, err := u.tokens.AccessToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("https://api.zoom.us/v2/meetings/%s", meetingID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("zoom user: delete meeting: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 204 success, 404 = already gone — both fine.
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("zoom user: delete meeting failed (%d): %s", resp.StatusCode, string(raw))
	}
	return nil
}

// staticTokenSource is a one-token TokenSource for tests / callers
// that already have the token in hand. Cached once so multiple calls
// in the same request don't surprise the caller with refresh churn.
type staticTokenSource struct {
	mu    sync.Mutex
	token string
}

// NewStaticTokenSource is a convenience for unit tests + the SDK
// signing endpoint (which doesn't actually call any Zoom API, but the
// API surface expects a TokenSource).
func NewStaticTokenSource(token string) TokenSource {
	return &staticTokenSource{token: token}
}

func (s *staticTokenSource) AccessToken(_ context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == "" {
		return "", ErrNoUserConnection
	}
	return s.token, nil
}
