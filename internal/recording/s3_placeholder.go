package recording

import (
	"context"
	"errors"
	"io"
	"time"
)

// errBackendNotConfigured is returned when a backend is not configured.
var errBackendNotConfigured = errors.New("recording: backend not configured")

// errBackendNotImplemented is returned when a backend is not yet implemented.
var errBackendNotImplemented = errors.New("recording: backend not yet implemented")

// s3Backend is a placeholder for S3-compatible recording backend.
//
// This backend is documented as pending in the roadmap and will be implemented
// in a future release. When S3 backend is configured (via recording.s3 section
// in config.yaml), the filesystem backend is used by default.
//
// To enable S3 support, add the following to your config.yaml:
//
//	s3:
//	  bucket_name: my-recordings
//	  region: us-east-1
//	  endpoint: s3.amazonaws.com
//	  access_key_id: ""
//	  secret_access_key: ""
//	  use_path_style_endpoint: false
//
// See: docs/development/roadmap.md#phase-5-recording-and-auditing
type s3Backend struct {
	// Configuration fields - populated at runtime when S3 is enabled
	bucketName string
	region     string
	enabled    bool
}

// NewS3Backend creates a new S3 backend placeholder.
// If cfg.Enabled is false (default), this returns a no-op backend that
// returns errBackendNotConfigured.
func NewS3Backend(cfg *struct {
	BucketName string
	Region     string
	Enabled    bool
}) *s3Backend {
	return &s3Backend{
		bucketName: cfg.BucketName,
		region:     cfg.Region,
		enabled:    cfg.Enabled,
	}
}

// UploadRecording implements recording.Store.
// Returns errBackendNotConfigured when S3 is not enabled.
// When S3 is enabled, this would upload to S3 storage.
func (b *s3Backend) UploadRecording(ctx context.Context, recording Meta, expiresIn time.Duration) (string, error) {
	if !b.enabled {
		return "", errBackendNotConfigured
	}
	return "", errBackendNotImplemented
}

// Open implements recording.Store.
// Returns errBackendNotConfigured when S3 is not enabled.
func (b *s3Backend) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if !b.enabled {
		return nil, errBackendNotConfigured
	}
	return nil, errBackendNotImplemented
}

// Remove implements recording.Store.
func (b *s3Backend) Remove(ctx context.Context, key string) error {
	if !b.enabled {
		return errBackendNotConfigured
	}
	return errBackendNotImplemented
}

// Verify implements recording.Store.
func (b *s3Backend) Verify(ctx context.Context, key, expectedSHA256 string) error {
	if !b.enabled {
		return errBackendNotConfigured
	}
	return errBackendNotImplemented
}

// Ensure s3Backend satisfies the recording.Store interfacevar _ Store = (*s3Backend)(nil)
