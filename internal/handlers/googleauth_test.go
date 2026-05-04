// White-box test — uses package handlers (not handlers_test) so we can
// inspect the unexported oauthCfg field directly.
package handlers

import (
	"testing"

	"golang.org/x/oauth2/google"

	"github.com/hngprojects/personal-trainer-be/internal/config"
)

func newTestAuthHandler(clientID, clientSecret, redirectURL string) *AuthHandler {
	return NewAuthHandler(nil, &config.Config{
		GoogleClientID:     clientID,
		GoogleClientSecret: clientSecret,
		GoogleRedirectURL:  redirectURL,
	}, nil)
}

func TestNewAuthHandler_NotNil(t *testing.T) {
	h := newTestAuthHandler("client-id", "client-secret", "http://localhost/callback")
	if h == nil {
		t.Fatal("expected non-nil AuthHandler")
	}
}

func TestNewAuthHandler_OAuthConfigCredentials(t *testing.T) {
	clientID := "test-client-id"
	clientSecret := "test-client-secret"
	redirectURL := "http://localhost:8080/auth/google/callback"

	h := newTestAuthHandler(clientID, clientSecret, redirectURL)

	if h.oauthCfg.ClientID != clientID {
		t.Errorf("expected ClientID %q, got %q", clientID, h.oauthCfg.ClientID)
	}
	if h.oauthCfg.ClientSecret != clientSecret {
		t.Errorf("expected ClientSecret %q, got %q", clientSecret, h.oauthCfg.ClientSecret)
	}
	if h.oauthCfg.RedirectURL != redirectURL {
		t.Errorf("expected RedirectURL %q, got %q", redirectURL, h.oauthCfg.RedirectURL)
	}
}

func TestNewAuthHandler_GoogleEndpoint(t *testing.T) {
	h := newTestAuthHandler("id", "secret", "http://localhost/callback")

	if h.oauthCfg.Endpoint != google.Endpoint {
		t.Errorf("expected Google OAuth endpoint, got %+v", h.oauthCfg.Endpoint)
	}
}

func TestNewAuthHandler_Scopes(t *testing.T) {
	h := newTestAuthHandler("id", "secret", "http://localhost/callback")

	expected := []string{"openid", "email", "profile"}
	if len(h.oauthCfg.Scopes) != len(expected) {
		t.Fatalf("expected %d scopes, got %d", len(expected), len(h.oauthCfg.Scopes))
	}
	for i, scope := range expected {
		if h.oauthCfg.Scopes[i] != scope {
			t.Errorf("expected scope[%d] = %q, got %q", i, scope, h.oauthCfg.Scopes[i])
		}
	}
}
