package recording

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// errS3NotConfigured is returned when S3 backend is not configured.
var errS3NotConfigured = errors.New("s3 recording backend not configured")

// errS3NotImplemented is returned when S3 feature is not yet implemented.
var errS3NotImplemented = errors.New("s3 recording backend not yet implemented")

// s3Backend implements the recording.Store interface for S3-compatible storage.
type s3Backend struct {
	bucket    string
	region    string
	enabled   bool
	minioClient *minio.Client
	endpoint  string
	accessKey string
	secretKey string
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
func NewS3Backend(cfg *S3Config) *s3Backend {
	creds := credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, "")
	opts := minio.Options{
		Creds:  creds,
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	}
	client, err := minio.New(cfg.Endpoint, &opts)
	if err != nil {
		// Return a disabled backend if client creation fails
		return &s3Backend{
			bucket: cfg.BucketName,
			region: cfg.Region,
			enabled: false,
			endpoint: cfg.Endpoint,
			accessKey: cfg.AccessKeyID,
			secretKey: cfg.SecretAccessKey,
		}
	}
	return &s3Backend{
		bucket:    cfg.BucketName,
		region:    cfg.Region,
		enabled:   true,
		minioClient: client,
		endpoint:  cfg.Endpoint,
		accessKey: cfg.AccessKeyID,
		secretKey: cfg.SecretAccessKey,
	}
}

// UploadRecording uploads an asciicast v2 recording to S3 storage.
// When S3 is not enabled, returns errS3NotConfigured.
// Returns a presigned URL for downloading the recording.
func (b *s3Backend) UploadRecording(ctx context.Context, recording Meta, expiresIn time.Duration) (string, error) {
	if !b.enabled {
		return "", errS3NotConfigured
	}

	// Generate a key based on timestamp and session ID
	now := time.Now().UTC()
	key := now.Format("2006/01/02") + "/" + recording.SessionID + ".cast"

	// Create a bytes buffer with the asciicast v2 header
	header := map[string]any{
		"version":   2,
		"width":     recording.Width,
		"height":    recording.Height,
		"timestamp": now.Unix(),
		"title":     recording.SessionID,
	}
	headerBytes, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("s3 header marshal failed: %w", err)
	}

	_, err = b.minioClient.PutObject(ctx, b.bucket, key, bytes.NewReader(headerBytes), int64(len(headerBytes)),
		minio.PutObjectOptions{
			ContentType: "application/x-asciicast",
			UserMetadata: map[string]string{
				"x-amz-meta-sha256": "",
			},
		})
	if err != nil {
		return "", fmt.Errorf("s3 upload failed: %w", err)
	}

	// Generate a presigned URL for download
	url, err := b.minioClient.PresignedGetObject(ctx, b.bucket, key, expiresIn, nil)
	if err != nil {
		return "", fmt.Errorf("s3 presign failed: %w", err)
	}
	return url.String(), nil
}

// Open implements recording.Store.
// Returns io.ReadCloser for downloading a recording from S3.
func (b *s3Backend) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if !b.enabled {
		return nil, errS3NotConfigured
	}

	obj, err := b.minioClient.GetObject(ctx, b.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("s3 download failed: %w", err)
	}
	return obj, nil
}

// Remove implements recording.Store.
func (b *s3Backend) Remove(ctx context.Context, key string) error {
	if !b.enabled {
		return errS3NotConfigured
	}

	err := b.minioClient.RemoveObject(ctx, b.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("s3 remove failed: %w", err)
	}
	return nil
}

// Verify implements recording.Store.
func (b *s3Backend) Verify(ctx context.Context, key, expectedSHA256 string) error {
	if !b.enabled {
		return errS3NotConfigured
	}

	// Download the object and compute SHA256
	reader, err := b.minioClient.GetObject(ctx, b.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("s3 verify download failed: %w", err)
	}
	defer reader.Close()

	h := sha256.New()
	if _, err := io.Copy(h, reader); err != nil {
		return fmt.Errorf("s3 verify hash failed: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))

	if got != expectedSHA256 {
		return fmt.Errorf("recording: integrity check failed for %s", key)
	}
	return nil
}

// Key returns the storage key prefix.
func (b *s3Backend) Key() string {
	return "s3://" + b.bucket + "/"
}

// writerForS3 is a recording.Writer that buffers data and uploads to S3 on Close.
type writerForS3 struct {
	mu         sync.Mutex
	bucket     string
	key        string
	minioClient *minio.Client
	buffer     *bytes.Buffer
	written    int64
	maxBytes   int64
	closed     bool
	truncated  bool
}

// Ensure writerForS3 satisfies the recording.Writer interface
var _ Writer = (*writerForS3)(nil)

// NewWriterForS3 creates a new writer that buffers recording data for S3 upload.
func NewWriterForS3(b *s3Backend, meta Meta) *writerForS3 {
	return &writerForS3{
		bucket:    b.bucket,
		key:       meta.SessionID + ".cast",
		minioClient: b.minioClient,
		buffer:    bytes.NewBuffer(nil),
		maxBytes:  256 << 20, // 256 MiB default
	}
}

// write writes data to the buffer.
func (w *writerForS3) write(p []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrLimit
	}
	if w.written+int64(len(p)) > w.maxBytes {
		w.truncated = true
		return ErrLimit
	}
	w.written += int64(len(p))
	_, err := w.buffer.Write(p)
	return err
}

// Output records terminal output.
func (w *writerForS3) Output(p []byte) error {
	return w.write(p)
}

// Input records terminal input.
func (w *writerForS3) Input(p []byte) error {
	return w.write(p)
}

// Close finalises the object by uploading to S3.
func (w *writerForS3) Close(_ context.Context) (Object, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return Object{}, errors.New("writer already closed")
	}
	w.closed = true

	// Compute SHA256 from the buffered data
	h := sha256.New()
	_, err := h.Write(w.buffer.Bytes())
	if err != nil {
		return Object{}, fmt.Errorf("s3 hash compute failed: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))

	// Upload the buffer to S3
	_, err = w.minioClient.PutObject(context.Background(), w.bucket, w.key, w.buffer, w.written,
		minio.PutObjectOptions{
			ContentType: "application/x-asciicast",
		})
	if err != nil {
		return Object{}, fmt.Errorf("s3 upload failed on close: %w", err)
	}

	return Object{
		Key:       w.key,
		Backend:   "s3",
		Format:    "asciicast-v2",
		Size:      w.written,
		SHA256:    got,
		Truncated: w.truncated,
	}, nil
}

// ListRecordings lists all recording objects in the S3 bucket.
func (b *s3Backend) ListRecordings(ctx context.Context, prefix string, recursive bool) ([]string, error) {
	if !b.enabled {
		return nil, errS3NotConfigured
	}

	var objects []string
	it := b.minioClient.ListObjectsIter(ctx, b.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: recursive,
	})
	for obj := range it {
		if obj.Err != nil {
			return nil, fmt.Errorf("s3 list error: %w", obj.Err)
		}
		if prefix != "" && !strings.HasPrefix(obj.Key, prefix) {
			continue
		}
		objects = append(objects, obj.Key)
	}
	return objects, nil
}

// Abort discards a partial recording.
func (w *writerForS3) Abort() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
}