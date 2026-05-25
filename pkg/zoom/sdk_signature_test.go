package zoom

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSDKSigner_NotConfigured(t *testing.T) {
	if (&SDKSigner{}).IsConfigured() {
		t.Fatal("empty signer should report not configured")
	}
	if NewSDKSigner("", "secret").IsConfigured() {
		t.Fatal("empty key should report not configured")
	}
	if NewSDKSigner("key", "").IsConfigured() {
		t.Fatal("empty secret should report not configured")
	}
	if _, err := (&SDKSigner{}).Sign("123", SDKRoleHost, 0); err != ErrSDKSignerNotConfigured {
		t.Fatalf("expected ErrSDKSignerNotConfigured, got %v", err)
	}
}

// Verify the JWT we produce matches Zoom Meeting SDK's spec: HS256
// over base64url(header) + "." + base64url(payload), and the payload
// carries the documented claim names.
func TestSDKSigner_JWTShape(t *testing.T) {
	s := NewSDKSigner("test-key", "test-secret")
	tok, err := s.Sign("9876543210", SDKRoleHost, 2*time.Hour)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT segments, got %d", len(parts))
	}

	// header check
	hdrJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("header b64: %v", err)
	}
	var hdr map[string]string
	if err := json.Unmarshal(hdrJSON, &hdr); err != nil {
		t.Fatalf("header json: %v", err)
	}
	if hdr["alg"] != "HS256" || hdr["typ"] != "JWT" {
		t.Fatalf("bad header: %v", hdr)
	}

	// payload check — names lifted from Zoom Meeting SDK docs
	plJSON, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var pl map[string]interface{}
	if err := json.Unmarshal(plJSON, &pl); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	for _, want := range []string{"appKey", "sdkKey", "mn", "role", "iat", "exp", "tokenExp"} {
		if _, ok := pl[want]; !ok {
			t.Fatalf("missing claim %q in %v", want, pl)
		}
	}
	if pl["sdkKey"] != "test-key" {
		t.Fatalf("sdkKey: want test-key got %v", pl["sdkKey"])
	}
	if pl["mn"] != "9876543210" {
		t.Fatalf("mn: want 9876543210, got %v", pl["mn"])
	}
	if int(pl["role"].(float64)) != int(SDKRoleHost) {
		t.Fatalf("role: want %d, got %v", SDKRoleHost, pl["role"])
	}

	// signature check
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte("test-secret"))
	mac.Write([]byte(signingInput))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if parts[2] != want {
		t.Fatalf("signature mismatch")
	}
}

// validFor=0 means "use the default", not "expire immediately".
// Regression guard for a subtle bug we saw in an earlier iteration.
func TestSDKSigner_DefaultValidity(t *testing.T) {
	s := NewSDKSigner("k", "s")
	tok, err := s.Sign("123", SDKRoleParticipant, 0)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	parts := strings.Split(tok, ".")
	plJSON, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var pl map[string]interface{}
	_ = json.Unmarshal(plJSON, &pl)
	iat := int64(pl["iat"].(float64))
	exp := int64(pl["exp"].(float64))
	delta := exp - iat
	if delta < int64((2*time.Hour - time.Minute).Seconds()) {
		t.Fatalf("default validity should be ~2h, got %ds", delta)
	}
}
