package cryptoutil

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

// helper: generate a base64-encoded 32-byte key
func mustKey(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func TestNewAESGCM_KeySizeEnforcement(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(make([]byte, 16)) // AES-128
	if _, err := NewAESGCM(short); err == nil {
		t.Fatal("expected error for 16-byte key, got nil")
	}

	bad := "not-base64!!"
	if _, err := NewAESGCM(bad); err == nil {
		t.Fatal("expected error for non-base64 key, got nil")
	}

	if _, err := NewAESGCM(mustKey(t)); err != nil {
		t.Fatalf("expected success for 32-byte key, got %v", err)
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	c, err := NewAESGCM(mustKey(t))
	if err != nil {
		t.Fatalf("NewAESGCM: %v", err)
	}

	cases := []string{
		"",
		"x",
		"hello world",
		strings.Repeat("A", 4096),
	}
	for _, p := range cases {
		ct, err := c.Encrypt([]byte(p))
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", p, err)
		}
		got, err := c.Decrypt(ct)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if string(got) != p {
			t.Fatalf("round-trip mismatch: want %q, got %q", p, string(got))
		}
	}
}

// Two encrypts of the same plaintext MUST produce different
// ciphertexts because the nonce is random per-call. If this ever
// regresses we've reintroduced nonce re-use under the same key, which
// breaks GCM's security guarantees.
func TestEncrypt_NonceFreshness(t *testing.T) {
	c, _ := NewAESGCM(mustKey(t))
	a, _ := c.Encrypt([]byte("plaintext"))
	b, _ := c.Encrypt([]byte("plaintext"))
	if a == b {
		t.Fatal("two encrypts produced identical ciphertext — nonce is being re-used")
	}
}

// Tampering with the ciphertext should fail the GCM auth tag check.
func TestDecrypt_TamperRejected(t *testing.T) {
	c, _ := NewAESGCM(mustKey(t))
	ct, _ := c.Encrypt([]byte("secret"))
	raw, _ := base64.StdEncoding.DecodeString(ct)
	// flip a single bit in the last byte (the tag)
	raw[len(raw)-1] ^= 0x01
	tampered := base64.StdEncoding.EncodeToString(raw)
	if _, err := c.Decrypt(tampered); err == nil {
		t.Fatal("expected tampered ciphertext to fail, got nil")
	}
}

// A ciphertext encrypted with one key must NOT decrypt under another.
func TestDecrypt_WrongKey(t *testing.T) {
	a, _ := NewAESGCM(mustKey(t))
	b, _ := NewAESGCM(mustKey(t))
	ct, _ := a.Encrypt([]byte("secret"))
	if _, err := b.Decrypt(ct); err == nil {
		t.Fatal("decrypt with different key should fail, got nil")
	}
}
