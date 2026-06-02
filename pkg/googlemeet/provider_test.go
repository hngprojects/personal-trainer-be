package googlemeet

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// happy path: CreateMeeting returns the meetingUri + space name.
func TestProvider_CreateMeeting(t *testing.T) {
	// Token endpoint returns a fixed bearer.
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "test-bearer", "expires_in": 3600})
	}))
	defer tokenSrv.Close()

	// Meet API: assert Authorization header + return a Spaces.create response.
	var sawAuth string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":        "spaces/abc123",
			"meetingUri":  "https://meet.google.com/xxx-yyyy-zzz",
			"meetingCode": "xxx-yyyy-zzz",
		})
	}))
	defer apiSrv.Close()

	c := NewOAuthClient("id", "secret", "rt", "")
	c.httpClient = tokenSrv.Client()
	p := NewProvider(c)
	p.httpClient = apiSrv.Client()

	withTokenURL(t, tokenSrv.URL, func() {
		withAPIBase(t, apiSrv.URL, func() {
			joinURL, mid, err := p.CreateMeeting(context.Background(), "ignored topic", time.Now(), 60)
			if err != nil {
				t.Fatalf("CreateMeeting: %v", err)
			}
			if joinURL != "https://meet.google.com/xxx-yyyy-zzz" {
				t.Fatalf("want meet URL, got %q", joinURL)
			}
			if mid != "spaces/abc123" {
				t.Fatalf("want space name as meetingID (used by DeleteMeeting), got %q", mid)
			}
		})
	})
	if sawAuth != "Bearer test-bearer" {
		t.Fatalf("Meet API didn't receive the bearer token, got %q", sawAuth)
	}
}

// Defensive: empty meetingUri in a 200 response must error rather
// than persisting a useless booking row.
func TestProvider_CreateMeetingEmptyResponseRejected(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
	}))
	defer tokenSrv.Close()
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"spaces/abc","meetingUri":""}`))
	}))
	defer apiSrv.Close()

	c := NewOAuthClient("id", "secret", "rt", "")
	c.httpClient = tokenSrv.Client()
	p := NewProvider(c)
	p.httpClient = apiSrv.Client()
	withTokenURL(t, tokenSrv.URL, func() {
		withAPIBase(t, apiSrv.URL, func() {
			_, _, err := p.CreateMeeting(context.Background(), "", time.Now(), 0)
			if err == nil {
				t.Fatal("want error for empty meetingUri, got nil")
			}
			if !strings.Contains(err.Error(), "empty") {
				t.Fatalf("error should mention empty fields, got: %v", err)
			}
		})
	})
}

// DeleteMeeting on a name without `spaces/` prefix must still work —
// callers pass whatever's in the booking row, which may have been
// written by an older code path.
func TestProvider_DeleteMeetingNormalizesNamePrefix(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
	}))
	defer tokenSrv.Close()

	var gotPath string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer apiSrv.Close()

	c := NewOAuthClient("id", "secret", "rt", "")
	c.httpClient = tokenSrv.Client()
	p := NewProvider(c)
	p.httpClient = apiSrv.Client()
	withTokenURL(t, tokenSrv.URL, func() {
		withAPIBase(t, apiSrv.URL, func() {
			if err := p.DeleteMeeting(context.Background(), "abc123"); err != nil {
				t.Fatalf("delete (bare name): %v", err)
			}
		})
	})
	if !strings.HasSuffix(gotPath, "/spaces/abc123:endActiveConference") {
		t.Fatalf("bare name not normalised to spaces/abc123; got path %q", gotPath)
	}
}

// 404 / "no active conference" responses must be treated as success.
// A booking that completed normally has no active conference left to
// end; failing the delete would block reschedule cleanup.
func TestProvider_DeleteMeetingTolerates404AndIdleSpace(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
	}))
	defer tokenSrv.Close()

	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"404 not found", http.StatusNotFound, ""},
		{"400 with `no active conference`", http.StatusBadRequest, `{"error":{"message":"no active conference"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer apiSrv.Close()
			c := NewOAuthClient("id", "secret", "rt", "")
			c.httpClient = tokenSrv.Client()
			p := NewProvider(c)
			p.httpClient = apiSrv.Client()
			withTokenURL(t, tokenSrv.URL, func() {
				withAPIBase(t, apiSrv.URL, func() {
					if err := p.DeleteMeeting(context.Background(), "spaces/abc"); err != nil {
						t.Fatalf("delete should succeed on %s, got: %v", tc.name, err)
					}
				})
			})
		})
	}
}

// Empty meetingID is a no-op — the booking flow passes "" when no
// meeting was ever attached (phone_callback / messenger paths). The
// provider must accept that without an extra error.
func TestProvider_DeleteMeetingEmptyIDIsNoop(t *testing.T) {
	p := NewProvider(NewOAuthClient("id", "secret", "rt", ""))
	if err := p.DeleteMeeting(context.Background(), ""); err != nil {
		t.Fatalf("empty meetingID must be no-op, got: %v", err)
	}
}

// IsConfigured mirrors the OAuth client. Builds on the OAuth tests
// for the truth table; here just the wire-through.
func TestProvider_IsConfiguredMirrorsOAuth(t *testing.T) {
	var p *Provider
	if p.IsConfigured() {
		t.Fatal("nil provider must report not configured")
	}
	configured := NewProvider(NewOAuthClient("id", "secret", "rt", ""))
	if !configured.IsConfigured() {
		t.Fatal("configured provider must report configured")
	}
	unconfigured := NewProvider(NewOAuthClient("", "", "", ""))
	if unconfigured.IsConfigured() {
		t.Fatal("provider with empty OAuth client must report not configured")
	}
}

// ── helpers ─────────────────────────────────────────────────────

func withAPIBase(t *testing.T, override string, fn func()) {
	t.Helper()
	orig := apiBaseRef
	apiBaseRef = override
	t.Cleanup(func() { apiBaseRef = orig })
	fn()
}
