// Package storage abstracts blob storage for user-uploaded content. The
// production implementation talks to MinIO (S3-compatible); a no-op variant
// lets the server start without storage configured (the upload endpoint will
// 503 rather than panic on the missing client).
package storage

import (
	"context"
	"errors"
	"io"
)

// ErrNotConfigured is returned by NoopStorage when any operation is attempted.
// Callers should check for this at the handler boundary and return 503.
var ErrNotConfigured = errors.New("storage backend is not configured")

// Storage is the consumer-side interface for object storage. Keep it minimal —
// only what the uploads package actually calls.
type Storage interface {
	// PutObject uploads body to the given key with the given content type.
	// Returns nil on success. The caller is responsible for constructing the
	// public URL — Storage does not concern itself with URL shape.
	PutObject(ctx context.Context, key string, body io.Reader, size int64, contentType string) error
}

// NoopStorage is wired in when MinIO env vars are missing so the rest of the
// app still builds and starts. Every call returns ErrNotConfigured.
type NoopStorage struct{}

func (NoopStorage) PutObject(_ context.Context, _ string, _ io.Reader, _ int64, _ string) error {
	return ErrNotConfigured
}
