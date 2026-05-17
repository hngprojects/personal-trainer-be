package storage

import (
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinioStorage is a MinIO/S3-backed Storage implementation. Construct via
// NewMinioStorage; it eagerly ensures the bucket exists so a misconfiguration
// surfaces at startup rather than on the first upload.
type MinioStorage struct {
	client *minio.Client
	bucket string
}

// NewMinioStorage builds a client against the given endpoint and ensures the
// bucket exists (creates it if missing). Returns an error if the client can't
// be constructed or the bucket check/create fails.
func NewMinioStorage(ctx context.Context, endpoint, accessKey, secretKey, bucket string, useSSL bool) (*MinioStorage, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: build minio client: %w", err)
	}

	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("storage: bucket exists check: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("storage: create bucket %q: %w", bucket, err)
		}
	}

	return &MinioStorage{client: client, bucket: bucket}, nil
}

// PutObject streams body into MinIO under the given key with the supplied
// content type. The size hint lets the SDK pick the best upload path (single
// PUT vs multipart) without buffering the whole body in memory.
func (s *MinioStorage) PutObject(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, body, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("storage: put %q: %w", key, err)
	}
	return nil
}
