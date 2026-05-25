package zoom

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SDKRole is the value Zoom's Meeting SDK expects in the JWT for who
// is joining. 1 = host (can start, end, manage), 0 = participant.
type SDKRole int

const (
	SDKRoleParticipant SDKRole = 0
	SDKRoleHost        SDKRole = 1
)

// SDKSigner mints the JWT the Zoom Meeting SDK requires to
// authenticate a client-side join. Per Zoom's docs, the JWT uses
// HS256 with the SDK secret and carries appKey/sdkKey + meeting
// number + role + iat/exp/tokenExp. We mint a fresh JWT for every
// join request rather than caching — they're cheap and the security
// model wants per-join attribution.
//
// See: https://developers.zoom.us/docs/meeting-sdk/auth/
type SDKSigner struct {
	sdkKey    string
	sdkSecret string
}

// NewSDKSigner returns a signer; nil if either credential is empty.
// Handlers check IsConfigured() and 503 when not, so boot doesn't
// silently expose a /sessions/{id}/join-info endpoint that can't
// actually sign.
func NewSDKSigner(sdkKey, sdkSecret string) *SDKSigner {
	return &SDKSigner{sdkKey: sdkKey, sdkSecret: sdkSecret}
}

func (s *SDKSigner) IsConfigured() bool {
	return s != nil && s.sdkKey != "" && s.sdkSecret != ""
}

// ErrSDKSignerNotConfigured is returned when the signer's credentials
// are missing — callers map this to 503.
var ErrSDKSignerNotConfigured = errors.New("zoom: SDK signer not configured")

// Sign builds the JWT for the given meeting + role. The JWT is valid
// from now to `validFor` from now; the SDK uses iat/exp + tokenExp.
// Recommended validFor is short — the JWT is needed only for the
// few seconds between the client receiving it and joining the meeting.
// 2 hours is the Zoom-recommended ceiling and what we use as the
// default.
func (s *SDKSigner) Sign(meetingNumber string, role SDKRole, validFor time.Duration) (string, error) {
	if !s.IsConfigured() {
		return "", ErrSDKSignerNotConfigured
	}
	if validFor <= 0 {
		validFor = 2 * time.Hour
	}

	now := time.Now().Unix()
	exp := time.Now().Add(validFor).Unix()

	header := map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	}
	// Field names lifted verbatim from Zoom's Meeting SDK auth docs —
	// `mn` (meeting number), `role` (host=1/attendee=0), `tokenExp`
	// (separate from `exp`; both required). `iat` must be present and
	// in the past — using `now` directly is fine.
	payload := map[string]interface{}{
		"appKey":   s.sdkKey,
		"sdkKey":   s.sdkKey,
		"mn":       meetingNumber,
		"role":     int(role),
		"iat":      now,
		"exp":      exp,
		"tokenExp": exp,
	}

	headerB64, err := jsonBase64URL(header)
	if err != nil {
		return "", err
	}
	payloadB64, err := jsonBase64URL(payload)
	if err != nil {
		return "", err
	}
	signingInput := headerB64 + "." + payloadB64

	mac := hmac.New(sha256.New, []byte(s.sdkSecret))
	mac.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + signature, nil
}

func jsonBase64URL(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("zoom sdk: marshal jwt segment: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
