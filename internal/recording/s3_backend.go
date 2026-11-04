package recording

import (
	"context"
	"errors"
	"io"
	"time"
)

// errS3NotConfigured is returned when S3 backend is not configured.
var errS3NotConfigured = errors.New("s3 recording backend not configured")

// errS3NotImplemented is returned when S3 feature is not yet implemented.
var errS3NotImplemented = errors.New("s3 recording backend not yet implemented")

// s3Backend implements the recording.Store interface for S3-compatible storage.
type s3Backend struct {
	bucket string
	region string
	enabled bool
}

// S3Config holds configuration for the S3 recording backend.
type S3Config struct {
	BucketName string `yaml:"bucket_name" env:"RSCON_S3_BUCKET"`
	Region     string `yaml:"region" env:"RSCON_S3_REGION"`
	Endpoint   string `yaml:"endpoint" env:"RSCON_S3_ENDPOINT"`
	// If empty, uses default AWS endpoints
	AccessKeyID      string `yaml:"access_key_id" env:"RSCON_S3_ACCESS_KEY_ID"`
	SecretAccessKey  string `yaml:"secret_access_key" env:"RSCON_S3_SECRET_ACCESS_KEY"`
	UsePathStyleEndpoint bool `yaml:"use_path_style_endpoint" env:"RSCON_S3_PATH_STYLE"`
	UseSSL           bool   `yaml:"use_ssl" env:"RSCON_S3_USE_SSL"`
}

// NewS3Backend creates a new S3 recording backend.
// If enabled is false (default), returns a no-op backend.
func NewS3Backend(cfg *struct {
	BucketName string
	Region     string
	Enabled    bool
}) *s3Backend {
	return &s3Backend{
		bucket: cfg.BucketName,
		region: cfg.Region,
		enabled: cfg.Enabled,
	}
}

// UploadRecording implements recording.Store.
// Uploads an asciicast v2 recording to S3 storage.
// When S3 is not enabled, returns errS3NotConfigured.
// When S3 is enabled, this would upload to S3 storage (pending implementation).
func (b *s3Backend) UploadRecording(ctx context.Context, recording Meta, expiresIn time.Duration) (string, error) {
	if !b.enabled {
		return "", errS3NotConfigured
	}
	return "", errS3NotImplemented
}

// Open implements recording.Store.
// Returns io.ReadCloser for downloading a recording.
// When S3 is not enabled, returns errS3NotConfigured.
func (b *s3Backend) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if !b.enabled {
		return nil, errS3NotConfigured
	}
	return nil, errS3NotImplemented
}

// Remove implements recording.Store.
func (b *s3Backend) Remove(ctx context.Context, key string) error {
	if !b.enabled {
		return errS3NotConfigured
	}
	return errS3NotImplemented
}

// Verify implements recording.Store.
func (b *s3Backend) Verify(ctx context.Context, key, expectedSHA256 string) error {
	if !b.enabled {
		return errS3NotConfigured
	}
	return errS3NotImplemented
}

// Key returns the storage key prefix.
func (b *s3Backend) Key() string {
	return "s3://" + b.bucket + "/"
}

// Ensure s3Backend satisfies the recording.Store interfacevar _ Store = (*s3Backend)(nil)
