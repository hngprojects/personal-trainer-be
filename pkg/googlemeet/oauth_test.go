package googlemeet

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// IsConfigured must be false on the empty zero value AND on a partial
// config (missing refresh token, missing secret, etc.). Boot guards
// rely on this — a half-configured OAuth client would otherwise
// silently 401 on every Meet call instead of cleanly 503ing.
func TestOAuthClient_IsConfigured(t *testing.T) {
	cases := []struct {
		name string
		c    *OAuthClient
		want bool
	}{
		{"nil receiver", nil, false},
		{"empty zero value", &OAuthClient{}, false},
		{"missing refresh token", NewOAuthClient("id", "secret", "", ""), false},
		{"missing client_secret", NewOAuthClient("id", "", "rt", ""), false},
		{"missing client_id", NewOAuthClient("", "secret", "rt", ""), false},
		{"all three set", NewOAuthClient("id", "secret", "rt", ""), true},
		{"all three set + host email", NewOAuthClient("id", "secret", "rt", "bot@x.com"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.IsConfigured(); got != tc.want {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

// invalid_grant from Google must surface as the distinct
// ErrTokenRevoked sentinel so handlers can write a useful operator-
// facing message ("re-run bootstrap") rather than a generic 500.
func TestOAuthClient_InvalidGrantSurfacesAsErrTokenRevoked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`))
	}))
	defer srv.Close()

	c := NewOAuthClient("id", "secret", "rt", "")
	c.httpClient = srv.Client()
	// Point the package-level tokenURL at the test server. Done via a
	// helper rather than mutating the global because Go test
	// parallelism could otherwise interfere.
	withTokenURL(t, srv.URL, func() {
		_, err := c.AccessToken(context.Background())
		if !errors.Is(err, ErrTokenRevoked) {
			t.Fatalf("want ErrTokenRevoked sentinel, got %v", err)
		}
	})
}

// Successful refresh: cached token returned + reused on subsequent
// calls until expiry-skew triggers a refetch. Verified by counting
// requests to the test server — second AccessToken call must NOT
// re-hit the network.
func TestOAuthClient_AccessTokenCached(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fresh-token",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	c := NewOAuthClient("id", "secret", "rt", "")
	c.httpClient = srv.Client()
	withTokenURL(t, srv.URL, func() {
		tok1, err := c.AccessToken(context.Background())
		if err != nil {
			t.Fatalf("first call: %v", err)
		}
		if tok1 != "fresh-token" {
			t.Fatalf("want fresh-token, got %q", tok1)
		}
		tok2, err := c.AccessToken(context.Background())
		if err != nil {
			t.Fatalf("second call: %v", err)
		}
		if tok2 != tok1 {
			t.Fatalf("cache miss: second call returned %q, want %q", tok2, tok1)
		}
		if hits != 1 {
			t.Fatalf("second call re-hit Google: hits=%d, want 1", hits)
		}
	})
}

// Generic 4xx that isn't invalid_grant should NOT be reported as
// ErrTokenRevoked — operators would chase the wrong issue.
func TestOAuthClient_OtherFailuresDoNotMimicTokenRevoked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("Service Unavailable"))
	}))
	defer srv.Close()
	c := NewOAuthClient("id", "secret", "rt", "")
	c.httpClient = srv.Client()
	withTokenURL(t, srv.URL, func() {
		_, err := c.AccessToken(context.Background())
		if err == nil {
			t.Fatal("want error, got nil")
		}
		if errors.Is(err, ErrTokenRevoked) {
			t.Fatalf("503 must NOT be reported as ErrTokenRevoked, got %v", err)
		}
		if !strings.Contains(err.Error(), "503") {
			t.Fatalf("want 503 in error message, got %v", err)
		}
	})
}

// AuthorizationURL must always request offline access + force a fresh
// consent. Without prompt=consent the refresh_token is omitted on
// re-consent — the bootstrap script's user would think it worked,
// paste nothing useful, and the integration would silently fail at
// runtime.
func TestAuthorizationURL_RequiresOfflineAndPromptConsent(t *testing.T) {
	url := AuthorizationURL("client-id-xyz", "http://localhost:8765/callback")
	for _, must := range []string{
		"access_type=offline",
		"prompt=consent",
		"client_id=client-id-xyz",
		"scope=https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fmeetings.space.created",
		"response_type=code",
	} {
		if !strings.Contains(url, must) {
			t.Fatalf("authorize URL missing %q\nfull URL: %s", must, url)
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────

// withTokenURL temporarily points the package-level tokenURL at the
// test server, runs fn, then restores the original. Not safe for
// parallel use within the same test — each test that uses it must
// stay serial, which they are by default.
func withTokenURL(t *testing.T, override string, fn func()) {
	t.Helper()
	orig := tokenURL
	tokenURL = override
	t.Cleanup(func() { tokenURL = orig })
	fn()
}
