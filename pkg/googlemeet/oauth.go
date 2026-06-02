// Package googlemeet wraps Google's Meet REST API for server-side
// minting of Meet rooms. ONE Workspace user's refresh token is held
// in env (MEET_REFRESH_TOKEN); every booking that picks
// session_platform=google_meet calls into here.
//
// Distinct from pkg/zoom on purpose: same conceptual job (mint a
// meeting URL on demand) but different wire formats, different auth
// model (refresh-token-only — no per-user OAuth dance), and different
// failure modes (Google's token-revocation surface is its own
// monster). Keeping them apart makes the per-platform behaviour easy
// to reason about.
package googlemeet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// MeetScope is the OAuth scope that lets `spaces.create` succeed.
// Workspace admins must allow this scope on the OAuth consent screen
// before the one-time bootstrap will work.
const MeetScope = "https://www.googleapis.com/auth/meetings.space.created"

// tokenURL is Google's OAuth token endpoint. Var (not const) so the
// test suite can point it at an httptest server; production callers
// never touch it.
var tokenURL = "https://oauth2.googleapis.com/token"

// authURL is the consent endpoint the bootstrap helper sends the
// admin to during the one-time setup. Defined here so the bootstrap
// command doesn't have to know any Google-specific URLs.
const authURL = "https://accounts.google.com/o/oauth2/v2/auth"

// ErrTokenRevoked is returned when Google rejects the refresh token
// with `invalid_grant`. Callers (the meeting provider) surface this
// as a 503 to the booking flow and operators know they need to re-run
// the bootstrap on a fresh refresh token.
var ErrTokenRevoked = errors.New("googlemeet: refresh token rejected by Google (invalid_grant) — re-run bootstrap")

// OAuthClient bundles the client credentials with a single refresh
// token. Refresh tokens issued to a Workspace user are intended to be
// long-lived; we cache the access token in memory + only re-fetch
// when it's about to expire.
type OAuthClient struct {
	clientID     string
	clientSecret string
	refreshToken string
	hostEmail    string // logged only; not sent to Google
	httpClient   *http.Client

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

func NewOAuthClient(clientID, clientSecret, refreshToken, hostEmail string) *OAuthClient {
	return &OAuthClient{
		clientID:     clientID,
		clientSecret: clientSecret,
		refreshToken: refreshToken,
		hostEmail:    hostEmail,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

// IsConfigured reports whether enough is present for an API call to
// even be attempted. Used as a boot-time guard and by the meeting
// provider's IsConfigured() so handlers can 503 cleanly when the
// integration isn't provisioned.
func (c *OAuthClient) IsConfigured() bool {
	return c != nil && c.clientID != "" && c.clientSecret != "" && c.refreshToken != ""
}

// HostEmail returns the Workspace user the refresh token belongs to,
// for log lines. Not the access-control surface — Google enforces
// that.
func (c *OAuthClient) HostEmail() string { return c.hostEmail }

// AccessToken returns a valid Bearer token for the Meet API. Cached
// in memory and refreshed automatically a minute before expiry so the
// hot path is always one map lookup, not a network call.
func (c *OAuthClient) AccessToken(ctx context.Context) (string, error) {
	if !c.IsConfigured() {
		return "", errors.New("googlemeet: OAuthClient not configured")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// 60s skew so an in-flight request can't race the clock and expire
	// mid-call (same buffer as the Zoom oauth client uses).
	if c.accessToken != "" && time.Now().Before(c.expiresAt.Add(-60*time.Second)) {
		return c.accessToken, nil
	}

	tok, exp, err := c.refresh(ctx)
	if err != nil {
		return "", err
	}
	c.accessToken = tok
	c.expiresAt = exp
	return tok, nil
}

// refresh swaps the long-lived refresh token for a short-lived access
// token. Holds no lock — caller (AccessToken) does the locking.
func (c *OAuthClient) refresh(ctx context.Context) (string, time.Time, error) {
	body := url.Values{}
	body.Set("client_id", c.clientID)
	body.Set("client_secret", c.clientSecret)
	body.Set("refresh_token", c.refreshToken)
	body.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("googlemeet: token refresh: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		// invalid_grant is the specific failure operators need to act
		// on (refresh token revoked; bootstrap helper must be re-run).
		// Distinguish from generic 4xx/5xx so the handler can show a
		// useful message without parsing JSON.
		if strings.Contains(string(raw), "invalid_grant") {
			return "", time.Time{}, ErrTokenRevoked
		}
		return "", time.Time{}, fmt.Errorf("googlemeet: token endpoint %d: %s", resp.StatusCode, string(raw))
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", time.Time{}, fmt.Errorf("googlemeet: decode token response: %w", err)
	}
	if parsed.AccessToken == "" {
		return "", time.Time{}, errors.New("googlemeet: token response missing access_token")
	}
	return parsed.AccessToken, time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second), nil
}

// AuthorizationURL is used by cmd/meet-bootstrap during the one-time
// setup that produces a refresh token. NOT used at runtime — the
// server only ever needs the refresh token. access_type=offline +
// prompt=consent is what guarantees Google returns a refresh token
// (without prompt=consent, repeat consents omit it).
func AuthorizationURL(clientID, redirectURI string) string {
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", MeetScope)
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	return authURL + "?" + q.Encode()
}

// ExchangeCode swaps an authorization code (returned to the redirect
// URI by Google during bootstrap) for a refresh token. Only used by
// cmd/meet-bootstrap.
func ExchangeCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (refreshToken string, err error) {
	body := url.Values{}
	body.Set("client_id", clientID)
	body.Set("client_secret", clientSecret)
	body.Set("code", code)
	body.Set("redirect_uri", redirectURI)
	body.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("googlemeet: bootstrap exchange: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("googlemeet: bootstrap exchange %d: %s", resp.StatusCode, string(raw))
	}
	var parsed struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("googlemeet: decode bootstrap response: %w", err)
	}
	if parsed.RefreshToken == "" {
		return "", errors.New("googlemeet: bootstrap returned no refresh_token — did you grant the scope, and is access_type=offline + prompt=consent set?")
	}
	return parsed.RefreshToken, nil
}
