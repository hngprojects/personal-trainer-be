// White-box test — uses package handlers (not handlers_test) so we can
// inspect the unexported oauthCfg field directly.
package handlers

import (
	"testing"

	"golang.org/x/oauth2/google"
)

func TestNewAuthHandler_NotNil(t *testing.T) {
	h := NewAuthHandler("client-id", "client-secret", "http://localhost/callback", nil)
	if h == nil {
		t.Fatal("expected non-nil AuthHandler")
	}
}

func TestNewAuthHandler_OAuthConfigCredentials(t *testing.T) {
	clientID := "test-client-id"
	clientSecret := "test-client-secret"
	redirectURL := "http://localhost:8080/auth/google/callback"

	h := NewAuthHandler(clientID, clientSecret, redirectURL, nil)

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
	h := NewAuthHandler("id", "secret", "http://localhost/callback", nil)

	if h.oauthCfg.Endpoint != google.Endpoint {
		t.Errorf("expected Google OAuth endpoint, got %+v", h.oauthCfg.Endpoint)
	}
}

func TestNewAuthHandler_Scopes(t *testing.T) {
	h := NewAuthHandler("id", "secret", "http://localhost/callback", nil)

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
