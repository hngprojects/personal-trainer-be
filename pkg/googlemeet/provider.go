package googlemeet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Meet REST API base. Versioning lives in the path; v2 is the
// current stable. Held in a var (not const) so the test suite can
// point it at an httptest server; production callers never touch it.
var apiBaseRef = "https://meet.googleapis.com/v2"

// Provider implements meeting.Provider via Google's Meet Spaces API.
//
// Why "Spaces" (not Calendar API): a Space is the Meet-native primitive
// — a meeting room that exists without an associated calendar event.
// We don't want server-created calendar events polluting anyone's
// schedule; we just want a join URL. Spaces give us that.
//
// One Provider instance is shared across all calls; the OAuthClient
// inside is goroutine-safe (mutex-guarded access-token cache).
type Provider struct {
	oauth      *OAuthClient
	httpClient *http.Client
}

func NewProvider(oauth *OAuthClient) *Provider {
	return &Provider{
		oauth:      oauth,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// IsConfigured reports whether the underlying OAuth client can mint
// an access token. Boot guards + handler 503s call this so the
// booking flow can refuse Meet bookings cleanly when the env hasn't
// been provisioned.
func (p *Provider) IsConfigured() bool {
	return p != nil && p.oauth.IsConfigured()
}

// CreateMeeting mints a fresh Meet space. Returns (joinURL, spaceName,
// error). spaceName has the form `spaces/abc123` and is the API
// identifier we'd need for DeleteMeeting; joinURL is the
// user-facing `https://meet.google.com/<code>` link we put in emails.
//
// topic, startTime, durationMinutes are ignored — Meet Spaces have no
// metadata for these. The signature matches meeting.Provider so the
// booking service can call any provider uniformly.
func (p *Provider) CreateMeeting(ctx context.Context, _ string, _ time.Time, _ int) (joinURL, meetingID string, err error) {
	tok, err := p.oauth.AccessToken(ctx)
	if err != nil {
		return "", "", err
	}

	// Empty body is fine; the API uses sensible defaults. We don't
	// override any of them: default access type is OPEN (anyone with
	// the link can join), default entry-point is HANGOUTS_MEET. Both
	// match what the booking flow needs.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBaseRef+"/spaces", bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("googlemeet: create space: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("googlemeet: create space %d: %s", resp.StatusCode, string(raw))
	}

	// Response shape (only fields we care about; Google's actual
	// payload has more):
	//   { "name": "spaces/abc", "meetingUri": "https://meet.google.com/xxx-yyyy-zzz", "meetingCode": "xxx-yyyy-zzz" }
	var parsed struct {
		Name        string `json:"name"`
		MeetingURI  string `json:"meetingUri"`
		MeetingCode string `json:"meetingCode"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", "", fmt.Errorf("googlemeet: decode space: %w", err)
	}
	if parsed.MeetingURI == "" || parsed.Name == "" {
		// Defensive: a 2xx with an empty body would otherwise persist
		// a booking row with a blank link. Fail loud, let the caller
		// reject the booking.
		return "", "", errors.New("googlemeet: create space returned empty meetingUri or name")
	}
	return parsed.MeetingURI, parsed.Name, nil
}

// DeleteMeeting ends the active conference on the space (if any).
// Spaces themselves persist by design in Google's model — they're
// reusable resources — so there's no "delete" verb. The closest
// operation is `endActiveConference`, which kicks everyone out and
// allows the space to be garbage-collected by Google's retention
// policy.
//
// meetingID is the `spaces/abc` name returned by CreateMeeting.
//
// 404 (space gone) and a no-op response (no active conference) are
// both treated as success — same defensive shape DeleteMeeting has on
// the Zoom side.
func (p *Provider) DeleteMeeting(ctx context.Context, meetingID string) error {
	if meetingID == "" {
		return nil
	}
	// Defensive: callers may pass either `spaces/abc` (the API name)
	// or just `abc`. Normalise so the URL we build is always valid.
	name := meetingID
	if !strings.HasPrefix(name, "spaces/") {
		name = "spaces/" + name
	}
	tok, err := p.oauth.AccessToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBaseRef+"/"+name+":endActiveConference", bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("googlemeet: end conference: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 200 = success, 404 = space gone, 400 with "no active conference"
	// = also fine (the meeting was already over). Anything else is a
	// real failure.
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusBadRequest && strings.Contains(string(raw), "no active conference") {
		return nil
	}
	return fmt.Errorf("googlemeet: end conference %d: %s", resp.StatusCode, string(raw))
}
