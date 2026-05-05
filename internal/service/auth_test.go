package service

import (
	"encoding/hex"
	"regexp"
	"testing"
)

var hexOnly = regexp.MustCompile(`^[0-9a-f]+$`)

func TestGenerateToken_Length(t *testing.T) {
	token, err := generateToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 32 random bytes → 64 hex characters
	if len(token) != hex.EncodedLen(32) {
		t.Errorf("expected length %d, got %d", hex.EncodedLen(32), len(token))
	}
}

func TestGenerateToken_Format(t *testing.T) {
	token, err := generateToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hexOnly.MatchString(token) {
		t.Errorf("token contains non-hex characters: %q", token)
	}
}

func TestGenerateToken_Uniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for range 100 {
		token, err := generateToken()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, dup := seen[token]; dup {
			t.Fatal("generateToken produced a duplicate token")
		}
		seen[token] = struct{}{}
	}
}

func TestValidatePassword_TooShort(t *testing.T) {
	cases := []string{"", "abc", "abc123", "1234567"}
	for _, pw := range cases {
		if err := validatePassword(pw); err == nil {
			t.Errorf("expected error for password %q, got nil", pw)
		}
	}
}

func TestValidatePassword_NoNumber(t *testing.T) {
	if err := validatePassword("abcdefgh"); err == nil {
		t.Error("expected error for password with no number")
	}
}

func TestValidatePassword_Valid(t *testing.T) {
	cases := []string{"Secret123", "pass1word", "12345678", "a1bcdefg"}
	for _, pw := range cases {
		if err := validatePassword(pw); err != nil {
			t.Errorf("unexpected error for valid password %q: %v", pw, err)
		}
	}
}
