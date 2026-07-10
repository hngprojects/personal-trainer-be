// Package apple — APNs (Apple Push Notification service) client.
//
// This is the direct-to-Apple push path that bypasses Firebase. For
// every iOS device token registered via POST /api/v1/register/device
// with platform=ios, internal/notification routes through this client
// instead of pkg/notification (FCM, still used for android/web).
//
// Why direct APNs:
//   - No Firebase dependency for iOS — one fewer vendor to operate.
//   - Lower latency: one HTTP/2 hop to Apple vs. backend → Firebase →
//     Apple.
//   - Per-token error reporting is first-party (Apple returns the
//     reason directly), so the auto-deactivate path we wired up
//     for FCM dead tokens (BadDeviceToken, Unregistered) maps cleanly.
//
// Trade-offs the caller should know:
//   - Mobile must send the APNs DEVICE token, NOT the FCM token. They
//     are different opaque strings produced by different APIs on the
//     iOS client. Switching is a one-line APNs SDK change on iOS plus
//     the registration call now stores the APNs token.
//   - We must distinguish sandbox (debug builds, TestFlight) from
//     production at runtime. APNs has two endpoints; sending to the
//     wrong one produces a BadDeviceToken error per message.
package apple

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/net/http2"
)

const (
	apnsProductionURL = "https://api.push.apple.com:443"
	apnsSandboxURL    = "https://api.sandbox.push.apple.com:443"

	// jwtTTL is how long each APNs bearer JWT lives before we re-mint
	// it. Apple's docs cap these at 60 minutes; we use 50 to leave a
	// safety margin against clock skew. Minting is cheap (one ES256
	// sign), so the cost of shorter is negligible.
	jwtTTL = 50 * time.Minute
)

// APNSEnvironment picks which Apple endpoint we POST to. Mobile builds
// signed against Apple's development profile (Xcode runs, TestFlight)
// produce sandbox tokens; App Store builds produce production tokens.
// Sending a token to the wrong environment fails per-message with
// BadDeviceToken — silent, opaque, and fairly common to get wrong, so
// boot logs surface the chosen environment.
type APNSEnvironment string

const (
	APNSProduction APNSEnvironment = "production"
	APNSSandbox    APNSEnvironment = "sandbox"
)

// APNSConfig is the static configuration. Same .p8 / Team ID / Key ID
// shape as SIWA (and the same physical file works for both if the key
// was created with both services ticked in Apple Developer → Keys).
type APNSConfig struct {
	TeamID        string          // Apple Developer team identifier (10-char)
	KeyID         string          // .p8 key id (10-char, Apple Developer → Keys)
	PrivateKeyPEM string          // raw .p8 contents
	BundleID      string          // iOS app bundle id, used as APNs `apns-topic` header
	Environment   APNSEnvironment // sandbox vs production
}

// IsZero reports whether the config is unset enough that we should
// skip wiring APNs entirely.
func (c APNSConfig) IsZero() bool {
	return c.TeamID == "" || c.KeyID == "" || c.PrivateKeyPEM == "" || c.BundleID == ""
}

// APNSClient is the public send surface. Construct once, share across
// callers — internally serialised by an HTTP/2 client which Apple
// rewards with connection reuse (mandatory on /3/device).
type APNSClient struct {
	cfg        APNSConfig
	endpoint   string
	httpClient *http.Client

	mu          sync.Mutex
	cachedJWT   string
	cachedJWTAt time.Time

	keyParseOnce sync.Once
	parsedKey    *ecdsa.PrivateKey
	keyParseErr  error
}

// NewAPNSClient parses the .p8 eagerly so config errors surface at
// boot. Runtime errors (network, Apple rejection) are returned from
// Send.
func NewAPNSClient(cfg APNSConfig) (*APNSClient, error) {
	if cfg.IsZero() {
		return nil, errors.New("apns: configuration is incomplete")
	}
	endpoint := apnsProductionURL
	if cfg.Environment == APNSSandbox {
		endpoint = apnsSandboxURL
	}

	// Explicitly construct an http2.Transport — APNs only speaks
	// HTTP/2, and using net/http's default transport sometimes
	// negotiates HTTP/1.1 first, producing opaque hang/timeout
	// failures on the first connection.
	t := &http2.Transport{}
	c := &APNSClient{
		cfg:        cfg,
		endpoint:   endpoint,
		httpClient: &http.Client{Transport: t, Timeout: 15 * time.Second},
	}
	if _, err := c.privateKey(); err != nil {
		return nil, fmt.Errorf("apns: %w", err)
	}
	return c, nil
}

// Payload is the standard APS dict + room for custom fields. Mirrors
// what we send via FCM today (alert title/body + sound + mutable
// content + a thread id if you want grouping); add fields as the
// mobile team needs them.
type Payload struct {
	Alert          PayloadAlert `json:"alert"`
	Badge          *int         `json:"badge,omitempty"`
	Sound          string       `json:"sound,omitempty"`
	MutableContent int          `json:"mutable-content,omitempty"`
	ThreadID       string       `json:"thread-id,omitempty"`
}

// PayloadAlert is the visible notification body.
type PayloadAlert struct {
	Title    string `json:"title,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
	Body     string `json:"body,omitempty"`
}

// SendResult is the per-token outcome of a single Send call. Mirrors
// the FCM SendResult shape so internal/notification can route either
// channel through the same per-token classification/auto-deactivate
// pipeline.
type SendResult struct {
	APNSID         string // Apple-assigned message id (from response header)
	StatusCode     int    // Apple's HTTP status
	Reason         string // Apple's reason code on failure (e.g. "BadDeviceToken")
	InvalidToken   bool   // true when the token should be deactivated
}

// Send posts a single notification to Apple. Synchronous — caller
// fan-outs for multi-device sends. Returns the per-token result for
// classification.
func (c *APNSClient) Send(ctx context.Context, deviceToken, title, body string) (*SendResult, error) {
	if strings.TrimSpace(deviceToken) == "" {
		return nil, errors.New("apns: empty device token")
	}

	payload := struct {
		APS Payload `json:"aps"`
	}{
		APS: Payload{
			Alert:          PayloadAlert{Title: title, Body: body},
			Sound:          "default",
			MutableContent: 1,
		},
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("apns: marshal payload: %w", err)
	}

	url := c.endpoint + "/3/device/" + deviceToken
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}

	tok, err := c.bearerJWT()
	if err != nil {
		return nil, fmt.Errorf("apns: jwt: %w", err)
	}
	req.Header.Set("authorization", "bearer "+tok)
	req.Header.Set("apns-topic", c.cfg.BundleID)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("content-type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apns: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	res := &SendResult{
		APNSID:     resp.Header.Get("apns-id"),
		StatusCode: resp.StatusCode,
	}

	if resp.StatusCode == http.StatusOK {
		return res, nil
	}

	// Non-200 — pull the reason from the body. Apple returns a JSON
	// object like `{"reason":"BadDeviceToken"}` on failure.
	respBody, _ := io.ReadAll(resp.Body)
	var er errorResponse
	_ = json.Unmarshal(respBody, &er)
	res.Reason = er.Reason
	res.InvalidToken = isPermanentlyInvalid(er.Reason, resp.StatusCode)
	return res, fmt.Errorf("apns: %d %s", resp.StatusCode, er.Reason)
}

type errorResponse struct {
	Reason string `json:"reason"`
}

// isPermanentlyInvalid mirrors the FCM classifier: "this token is
// dead, deactivate the row" vs. "transient, retry later". Apple's
// reason codes are stable strings documented in their developer docs.
//
// We DELIBERATELY exclude `DeviceTokenNotForTopic` from the
// "permanently invalid" set. It looks device-shaped but is almost
// always a server-side config mismatch — wrong APPLE_BUNDLE_ID vs
// what the mobile client is registered for, or a
// sandbox-token-against-production-endpoint (or vice versa) mixup.
// If we treat it as dead we'd mass-deactivate every iOS device row
// on the first send after a bad deploy, forcing every user to
// re-register push before it worked again. Better to log noisily,
// leave the rows active, and let the operator fix the config.
func isPermanentlyInvalid(reason string, status int) bool {
	if status == http.StatusGone { // 410: token has been removed
		return true
	}
	switch reason {
	case "BadDeviceToken", "Unregistered":
		return true
	}
	return false
}

// bearerJWT returns the cached JWT if it's still inside the 50-minute
// window, otherwise mints a new one.
func (c *APNSClient) bearerJWT() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cachedJWT != "" && time.Since(c.cachedJWTAt) < jwtTTL-1*time.Minute {
		return c.cachedJWT, nil
	}
	key, err := c.privateKey()
	if err != nil {
		return "", err
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": c.cfg.TeamID,
		"iat": now.Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["kid"] = c.cfg.KeyID
	signed, err := tok.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("sign jwt: %w", err)
	}
	c.cachedJWT = signed
	c.cachedJWTAt = now
	return signed, nil
}

func (c *APNSClient) privateKey() (*ecdsa.PrivateKey, error) {
	c.keyParseOnce.Do(func() {
		block, _ := pem.Decode([]byte(c.cfg.PrivateKeyPEM))
		if block == nil {
			c.keyParseErr = errors.New("PEM decode failed")
			return
		}
		raw, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			ec, ecErr := x509.ParseECPrivateKey(block.Bytes)
			if ecErr != nil {
				c.keyParseErr = fmt.Errorf("parse private key: %w (EC: %v)", err, ecErr)
				return
			}
			c.parsedKey = ec
			return
		}
		key, ok := raw.(*ecdsa.PrivateKey)
		if !ok {
			c.keyParseErr = errors.New("private key is not ECDSA — APNs requires ES256")
			return
		}
		c.parsedKey = key
	})
	return c.parsedKey, c.keyParseErr
}
