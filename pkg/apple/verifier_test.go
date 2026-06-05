package apple_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/hngprojects/personal-trainer-be/pkg/apple"
)

// signedToken produces an Apple-shaped identity token signed with key,
// then returns the compact JWS string. claims overrides win over the
// defaults, so a test passing claims["aud"] = "bad" stamps an explicit
// audience without us having to also re-supply iss/sub/etc.
func signedToken(t *testing.T, key *rsa.PrivateKey, overrides jwt.MapClaims) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"iss":              apple.AppleIssuer,
		"aud":              "com.fitcal.app",
		"sub":              "001234.abc.xyz",
		"iat":              now.Unix(),
		"exp":              now.Add(10 * time.Minute).Unix(),
		"email":            "user@example.com",
		"email_verified":   "true",
		"is_private_email": "false",
	}
	for k, v := range overrides {
		claims[k] = v
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func newTestVerifier(t *testing.T, key *rsa.PrivateKey, auds []string) *apple.Verifier {
	t.Helper()
	kf := func(_ *jwt.Token) (interface{}, error) {
		return &key.PublicKey, nil
	}
	v, err := apple.NewVerifier(context.Background(), auds, apple.WithKeyfunc(kf))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return v
}

func TestVerifier_AcceptsValidToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa gen: %v", err)
	}
	v := newTestVerifier(t, key, []string{"com.fitcal.app"})
	tok := signedToken(t, key, nil)

	got, err := v.Verify(context.Background(), tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Sub != "001234.abc.xyz" {
		t.Errorf("sub: got %q want %q", got.Sub, "001234.abc.xyz")
	}
	if got.Email != "user@example.com" {
		t.Errorf("email: got %q", got.Email)
	}
	if !got.EmailVerified {
		t.Errorf("email_verified should be true (string 'true' Apple shape)")
	}
	if got.IsPrivateEmail {
		t.Errorf("is_private_email should be false")
	}
}

func TestVerifier_RejectsWrongAudience(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	v := newTestVerifier(t, key, []string{"com.fitcal.app"})
	tok := signedToken(t, key, jwt.MapClaims{"aud": "com.someone-else.app"})

	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestVerifier_RejectsExpired(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	v := newTestVerifier(t, key, []string{"com.fitcal.app"})
	past := time.Now().Add(-1 * time.Hour).Unix()
	tok := signedToken(t, key, jwt.MapClaims{"exp": past, "iat": past - 60})

	_, err := v.Verify(context.Background(), tok)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
	if !errors.Is(err, jwt.ErrTokenExpired) {
		t.Logf("(non-fatal) expected ErrTokenExpired in chain, got %v — still rejected which is the contract", err)
	}
}

func TestVerifier_RejectsWrongIssuer(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	v := newTestVerifier(t, key, []string{"com.fitcal.app"})
	tok := signedToken(t, key, jwt.MapClaims{"iss": "https://impostor.example/"})

	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestVerifier_AcceptsIssuerWithTrailingSlash(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	v := newTestVerifier(t, key, []string{"com.fitcal.app"})
	tok := signedToken(t, key, jwt.MapClaims{"iss": apple.AppleIssuerWithSlash})

	if _, err := v.Verify(context.Background(), tok); err != nil {
		t.Fatalf("trailing-slash issuer should be accepted: %v", err)
	}
}

func TestVerifier_AcceptsAudienceFromArray(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	v := newTestVerifier(t, key, []string{"com.fitcal.app"})
	tok := signedToken(t, key, jwt.MapClaims{"aud": []interface{}{"com.fitcal.app", "com.fitcal.app.web"}})

	if _, err := v.Verify(context.Background(), tok); err != nil {
		t.Fatalf("aud as array should be accepted: %v", err)
	}
}

func TestVerifier_RejectsMissingSub(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	v := newTestVerifier(t, key, []string{"com.fitcal.app"})
	tok := signedToken(t, key, jwt.MapClaims{"sub": ""})

	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Fatal("expected error for empty sub")
	}
}

func TestVerifier_DetectsPrivateRelayByDomain(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	v := newTestVerifier(t, key, []string{"com.fitcal.app"})
	// is_private_email omitted, but the email shape says private relay.
	tok := signedToken(t, key, jwt.MapClaims{
		"email":            "abc123@privaterelay.appleid.com",
		"is_private_email": nil,
	})

	got, err := v.Verify(context.Background(), tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !got.IsPrivateEmail {
		t.Errorf("privaterelay.appleid.com address should be flagged as private")
	}
}

func TestVerifier_RejectsBadSignature(t *testing.T) {
	signKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	verifyKey, _ := rsa.GenerateKey(rand.Reader, 2048)

	kf := func(_ *jwt.Token) (interface{}, error) {
		return &verifyKey.PublicKey, nil
	}
	v, err := apple.NewVerifier(context.Background(), []string{"com.fitcal.app"}, apple.WithKeyfunc(kf))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	tok := signedToken(t, signKey, nil)

	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Fatal("expected signature verification failure")
	}
}

func TestVerifier_RejectsHS256(t *testing.T) {
	// Apple uses RS256 only — a token signed with HS256 must be
	// rejected regardless of whether we'd accept the audience etc.,
	// otherwise an attacker who guesses the key material could mint
	// arbitrary tokens.
	key := []byte("not-a-real-secret")
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": apple.AppleIssuer,
		"aud": "com.fitcal.app",
		"sub": "001234",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign hs256: %v", err)
	}

	kf := func(_ *jwt.Token) (interface{}, error) { return key, nil }
	v, err := apple.NewVerifier(context.Background(), []string{"com.fitcal.app"}, apple.WithKeyfunc(kf))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	if _, err := v.Verify(context.Background(), signed); err == nil {
		t.Fatal("HS256 token must be rejected even if signature 'verifies'")
	}
}

func TestNewVerifier_RejectsEmptyAudienceList(t *testing.T) {
	if _, err := apple.NewVerifier(context.Background(), nil, apple.WithKeyfunc(func(*jwt.Token) (interface{}, error) { return nil, nil })); err == nil {
		t.Fatal("expected NewVerifier to reject empty audience list")
	}
	if _, err := apple.NewVerifier(context.Background(), []string{"   ", ""}, apple.WithKeyfunc(func(*jwt.Token) (interface{}, error) { return nil, nil })); err == nil {
		t.Fatal("expected NewVerifier to reject whitespace-only audiences")
	}
}
