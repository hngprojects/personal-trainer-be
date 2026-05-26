// Package cryptoutil offers small symmetric-encryption helpers used by
// callers that need to store sensitive blobs at rest. The only
// algorithm exposed today is AES-256-GCM, chosen for two reasons:
//   - authenticated (a tampered ciphertext fails to decrypt rather
//     than producing garbage plaintext the caller may not check)
//   - in Go stdlib (no extra dependency to vet)
//
// The current call site is the Zoom OAuth refresh-token store; treat
// this as a general-purpose helper to discourage hand-rolling crypto
// in each package that needs it.
package cryptoutil

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// ErrInvalidKey is returned when ParseAESKey can't decode the
// configured key into a 32-byte AES-256 key.
var ErrInvalidKey = errors.New("cryptoutil: key must be 32 bytes (AES-256) when base64-decoded")

// AESGCM bundles a single key with its derived AEAD instance so the
// hot encrypt/decrypt path doesn't re-derive on every call.
type AESGCM struct {
	aead cipher.AEAD
}

// NewAESGCM constructs the helper from a base64-encoded 32-byte key.
// Generate one with `openssl rand -base64 32`. Returns ErrInvalidKey
// for any other size — AES-128 / AES-192 are intentionally unsupported
// to keep the algorithm choice unambiguous across the codebase.
func NewAESGCM(base64Key string) (*AESGCM, error) {
	raw, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return nil, fmt.Errorf("cryptoutil: decode key: %w", err)
	}
	if len(raw) != 32 {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, fmt.Errorf("cryptoutil: build cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cryptoutil: build gcm: %w", err)
	}
	return &AESGCM{aead: aead}, nil
}

// Encrypt returns base64(nonce || ciphertext || tag). The nonce is
// regenerated per call from crypto/rand — re-using a nonce under the
// same key is catastrophic for GCM, so callers MUST NOT cache or
// derive nonces themselves.
func (c *AESGCM) Encrypt(plaintext []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("cryptoutil: read nonce: %w", err)
	}
	// Seal returns nonce-less ciphertext+tag; we prepend the nonce so
	// Decrypt can split them. Same prefix-shape Go's tls package uses
	// internally, so it's a familiar layout to reviewers.
	sealed := c.aead.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, 0, len(nonce)+len(sealed))
	out = append(out, nonce...)
	out = append(out, sealed...)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt inverts Encrypt. Returns an error if the input fails the
// GCM auth tag check (tampered ciphertext, wrong key, truncated input,
// etc.) — callers should treat any error as "corrupted" and surface
// without revealing details.
func (c *AESGCM) Decrypt(encoded string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("cryptoutil: decode ciphertext: %w", err)
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns+c.aead.Overhead() {
		return nil, errors.New("cryptoutil: ciphertext too short")
	}
	nonce, sealed := raw[:ns], raw[ns:]
	plain, err := c.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, fmt.Errorf("cryptoutil: open: %w", err)
	}
	return plain, nil
}
