package apple

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SIWA = Sign in with Apple. This file implements the server-to-server
// parts of Apple's OAuth: minting a `client_secret` JWT, exchanging an
// `authorization_code` for a refresh token, and revoking the refresh
// token when a user deletes their account.
//
// The identity-token verifier in verifier.go is unchanged and is still
// the primary auth path. This package adds the OPTIONAL channel that
// lets the backend talk to Apple directly when needed.
//
// Apple's account-deletion guideline (App Store Review Guideline 5.1.1
// (v)) requires apps that support account deletion to ALSO revoke the
// Apple-issued tokens. Without it, Apple keeps the user listed under
// "Apps using your Apple ID" and the app can be rejected on the next
// submission.

const (
	// appleTokenURL is Apple's public token endpoint. The auth-code
	// exchange POSTs here. Stored as a var so tests can swap it.
	appleTokenURLDefault = "https://appleid.apple.com/auth/token"

	// appleRevokeURL is Apple's revocation endpoint. Same swap-for-tests
	// pattern.
	appleRevokeURLDefault = "https://appleid.apple.com/auth/revoke"

	// clientSecretTTL is how long each signed client_secret JWT lasts
	// before we re-mint it. Apple caps client_secret JWTs at 6 months
	// (15777000 seconds). We use 5 months to leave a safety buffer
	// against clock skew and any short outages — minting is cheap, so
	// shortening this isn't a meaningful cost.
	clientSecretTTL = 5 * 30 * 24 * time.Hour // ≈ 5 months
)

// SIWAOAuthConfig is the static configuration needed to talk to Apple
// on the OAuth side. Sourced from env at boot; tested with literals.
type SIWAOAuthConfig struct {
	// TeamID is the 10-character Apple Developer team identifier
	// (Apple Developer → Membership). Used as the JWT `iss`.
	TeamID string

	// ClientID is the iOS app's bundle ID (for native sign-in) OR the
	// Services ID (for web/Android). Used as the JWT `sub` and as the
	// `client_id` form param on the token + revoke endpoints.
	ClientID string

	// KeyID is the 10-character ID of the .p8 (Apple Developer → Keys).
	// Goes in the JWT header as `kid`. The same key must have "Sign in
	// with Apple" ticked on it.
	KeyID string

	// PrivateKeyPEM is the literal contents of the .p8 file — starts
	// with `-----BEGIN PRIVATE KEY-----`. Loaders that accept base64
	// should decode before constructing this struct.
	PrivateKeyPEM string
}

// IsZero reports whether the config is unset enough that we should
// skip the OAuth side entirely. Used at boot to decide whether to wire
// the revocation path at all.
func (c SIWAOAuthConfig) IsZero() bool {
	return c.TeamID == "" || c.ClientID == "" || c.KeyID == "" || c.PrivateKeyPEM == ""
}

// SIWAOAuth holds the live OAuth client. Construct once, call many.
// Concurrent calls are safe — the only shared state is the
// client_secret cache and that's mutex-guarded.
type SIWAOAuth struct {
	cfg        SIWAOAuthConfig
	httpClient *http.Client

	tokenURL  string // injectable for tests
	revokeURL string // injectable for tests

	mu                sync.Mutex
	cachedSecret      string
	cachedSecretExp   time.Time
	parsedPrivateKey  *ecdsa.PrivateKey
	keyParseOnce      sync.Once
	keyParseErr       error
}

// NewSIWAOAuth returns a ready client. Returns an error only on
// invalid PEM — runtime auth failures are returned from the per-call
// methods so a transient Apple outage doesn't poison the constructor.
func NewSIWAOAuth(cfg SIWAOAuthConfig) (*SIWAOAuth, error) {
	if cfg.IsZero() {
		return nil, errors.New("siwa oauth: configuration is incomplete")
	}
	c := &SIWAOAuth{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		tokenURL:   appleTokenURLDefault,
		revokeURL:  appleRevokeURLDefault,
	}
	// Eagerly parse + cache the private key so config errors surface at
	// boot rather than on the first revoke call (which would silently
	// leak the failure into a user-facing flow).
	if _, err := c.privateKey(); err != nil {
		return nil, fmt.Errorf("siwa oauth: %w", err)
	}
	return c, nil
}

// TokenResponse is the subset of Apple's token-endpoint response that
// we actually consume. Apple returns more fields (access_token,
// id_token, token_type, expires_in) that we leave out — they're
// usable in principle but we only need the refresh token for the
// revocation flow.
type TokenResponse struct {
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// ExchangeAuthCode trades the authorization_code Apple delivered to
// the mobile client at sign-in for a refresh_token. Backend stores
// the refresh_token (encrypted at rest) so it can later call Revoke
// on it when the user deletes their account.
//
// Apple issues an authorization_code ONCE per sign-in. The mobile app
// must capture it from `ASAuthorizationAppleIDCredential.authorizationCode`
// and send it on the FIRST sign-in. Subsequent sign-ins don't carry
// a fresh code, so call this only when the field is present.
func (c *SIWAOAuth) ExchangeAuthCode(ctx context.Context, authCode string) (*TokenResponse, error) {
	secret, err := c.clientSecret()
	if err != nil {
		return nil, fmt.Errorf("client_secret: %w", err)
	}

	form := url.Values{}
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", secret)
	form.Set("code", authCode)
	form.Set("grant_type", "authorization_code")

	return c.postForm(ctx, c.tokenURL, form, true)
}

// Revoke tells Apple to invalidate a refresh_token (and any access
// tokens derived from it). Called when a SIWA user deletes their
// account. Apple's docs say revocation is asynchronous — the call
// returns 200 once accepted, and the actual invalidation happens
// server-side.
//
// Best-effort by contract: account deletion should NOT block on
// Apple's API being reachable. Callers log and proceed on error.
func (c *SIWAOAuth) Revoke(ctx context.Context, refreshToken string) error {
	if strings.TrimSpace(refreshToken) == "" {
		return errors.New("siwa oauth: empty refresh token")
	}
	secret, err := c.clientSecret()
	if err != nil {
		return fmt.Errorf("client_secret: %w", err)
	}

	form := url.Values{}
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", secret)
	form.Set("token", refreshToken)
	form.Set("token_type_hint", "refresh_token")

	// The revoke endpoint returns no body on success. postForm wants a
	// TokenResponse so it can return one — pass requireBody=false so
	// it doesn't error on the empty 200.
	_, err = c.postForm(ctx, c.revokeURL, form, false)
	return err
}

// clientSecret returns a cached JWT if it's still fresh, else mints a
// new one. Apple caps client_secret JWTs at 6 months; we cap ours at
// 5 to keep a refresh buffer.
func (c *SIWAOAuth) clientSecret() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cachedSecret != "" && time.Now().Before(c.cachedSecretExp.Add(-1*time.Hour)) {
		return c.cachedSecret, nil
	}

	key, err := c.privateKey()
	if err != nil {
		return "", err
	}

	now := time.Now()
	exp := now.Add(clientSecretTTL)
	claims := jwt.MapClaims{
		"iss": c.cfg.TeamID,
		"iat": now.Unix(),
		"exp": exp.Unix(),
		"aud": "https://appleid.apple.com",
		"sub": c.cfg.ClientID,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["kid"] = c.cfg.KeyID
	signed, err := tok.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("sign client_secret: %w", err)
	}
	c.cachedSecret = signed
	c.cachedSecretExp = exp
	return signed, nil
}

func (c *SIWAOAuth) privateKey() (*ecdsa.PrivateKey, error) {
	c.keyParseOnce.Do(func() {
		block, _ := pem.Decode([]byte(c.cfg.PrivateKeyPEM))
		if block == nil {
			c.keyParseErr = errors.New("PEM decode failed (private key is malformed)")
			return
		}
		raw, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			// Some Apple .p8 files are EC-shaped; try the EC-specific
			// parser as a fallback before giving up.
			ec, ecErr := x509.ParseECPrivateKey(block.Bytes)
			if ecErr != nil {
				c.keyParseErr = fmt.Errorf("parse private key: %w (also tried EC: %v)", err, ecErr)
				return
			}
			c.parsedPrivateKey = ec
			return
		}
		key, ok := raw.(*ecdsa.PrivateKey)
		if !ok {
			c.keyParseErr = errors.New("private key is not ECDSA — Apple SIWA requires ES256")
			return
		}
		c.parsedPrivateKey = key
	})
	if c.keyParseErr != nil {
		return nil, c.keyParseErr
	}
	return c.parsedPrivateKey, nil
}

// postForm POSTs a URL-encoded body and decodes the JSON response.
// requireBody=true returns an error on an empty 200 (the token
// endpoint), false treats empty 200 as success (the revoke endpoint).
func (c *SIWAOAuth) postForm(ctx context.Context, endpoint string, form url.Values, requireBody bool) (*TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apple request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("apple endpoint %s returned %d: %s", endpoint, resp.StatusCode, truncate(string(body), 200))
	}
	if !requireBody && len(body) == 0 {
		return nil, nil
	}
	var tr TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("decode apple response: %w", err)
	}
	if requireBody && tr.RefreshToken == "" {
		return nil, fmt.Errorf("apple token endpoint returned no refresh_token (body: %s)", truncate(string(body), 200))
	}
	return &tr, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
