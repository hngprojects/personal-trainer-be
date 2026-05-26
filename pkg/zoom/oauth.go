package zoom

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuthClient drives the per-user authorization-code flow distinct
// from the server-to-server account_credentials grant in this same
// package's Client. Different Zoom marketplace app type, different
// credentials, different lifecycle — kept in its own type so the two
// don't accidentally share refresh paths.
type OAuthClient struct {
	clientID     string
	clientSecret string
	redirectURL  string
	httpClient   *http.Client
}

func NewOAuthClient(clientID, clientSecret, redirectURL string) *OAuthClient {
	return &OAuthClient{
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURL:  redirectURL,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

// IsConfigured returns true iff every field needed to actually run an
// OAuth round-trip is set. Handlers check this and 503 when false so
// boot doesn't silently expose a half-configured connect endpoint.
func (c *OAuthClient) IsConfigured() bool {
	return c != nil && c.clientID != "" && c.clientSecret != "" && c.redirectURL != ""
}

// AuthorizationURL builds the URL the user is redirected to in order
// to consent. State must be a CSRF-resistant value the caller can
// verify on return (handler generates a random string, stores it in
// the user's session/redis, checks it in the callback).
func (c *OAuthClient) AuthorizationURL(state string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", c.clientID)
	q.Set("redirect_uri", c.redirectURL)
	q.Set("state", state)
	return "https://zoom.us/oauth/authorize?" + q.Encode()
}

// TokenResponse mirrors Zoom's OAuth token endpoint shape. Scope is
// space-separated values per RFC 6749.
type TokenResponse struct {
	AccessToken  string
	RefreshToken string
	Scope        string
	ExpiresAt    time.Time
}

// ExchangeCode swaps the authorization code from the callback for a
// token pair. Called once per Connect flow.
func (c *OAuthClient) ExchangeCode(ctx context.Context, code string) (*TokenResponse, error) {
	body := url.Values{}
	body.Set("grant_type", "authorization_code")
	body.Set("code", code)
	body.Set("redirect_uri", c.redirectURL)
	return c.postTokenEndpoint(ctx, body)
}

// Refresh swaps the current refresh token for a new access+refresh
// pair. Zoom uses rolling refresh tokens — the OLD refresh token is
// invalidated on success, so callers MUST persist the new one before
// using the new access token. A crash between refresh-success and
// persist would lose the connection entirely.
func (c *OAuthClient) Refresh(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	body := url.Values{}
	body.Set("grant_type", "refresh_token")
	body.Set("refresh_token", refreshToken)
	return c.postTokenEndpoint(ctx, body)
}

func (c *OAuthClient) postTokenEndpoint(ctx context.Context, body url.Values) (*TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://zoom.us/oauth/token", strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.clientID, c.clientSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zoom oauth: token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("zoom oauth: token endpoint %d: %s", resp.StatusCode, string(raw))
	}
	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("zoom oauth: decode token response: %w", err)
	}
	if raw.AccessToken == "" || raw.RefreshToken == "" {
		return nil, fmt.Errorf("zoom oauth: token response missing access or refresh token")
	}
	return &TokenResponse{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		Scope:        raw.Scope,
		// Subtract a small buffer so refresh fires BEFORE the token
		// actually expires — protects against clock skew between us
		// and Zoom + the few-second window an in-flight request
		// might take.
		ExpiresAt: time.Now().Add(time.Duration(raw.ExpiresIn-60) * time.Second),
	}, nil
}

// UserProfile is the subset of Zoom's /users/me response we care about
// — enough to populate the audit fields in user_zoom_credentials.
type UserProfile struct {
	ID        string
	AccountID string
	Email     string
}

// FetchUserProfile calls Zoom's /users/me with the access token to
// learn the Zoom-side identifiers we record alongside the encrypted
// credentials. Called once per Connect flow, right after ExchangeCode.
func (c *OAuthClient) FetchUserProfile(ctx context.Context, accessToken string) (*UserProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.zoom.us/v2/users/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zoom oauth: fetch user: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("zoom oauth: /users/me %d: %s", resp.StatusCode, string(raw))
	}
	var raw struct {
		ID        string `json:"id"`
		AccountID string `json:"account_id"`
		Email     string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("zoom oauth: decode /users/me: %w", err)
	}
	return &UserProfile{ID: raw.ID, AccountID: raw.AccountID, Email: raw.Email}, nil
}
