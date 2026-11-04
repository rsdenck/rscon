// Package recording captures interactive SSH sessions as asciicast v2 files
// with SHA-256 integrity metadata.
package recording

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrLimit is returned when a recording exceeds the configured size limit.
var ErrLimit = errors.New("recording size limit exceeded")

// Object describes a stored recording.
type Object struct {
	Key       string `json:"object_key"`
	Backend   string `json:"backend"`
	Format    string `json:"format"`
	Size      int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
	Truncated bool   `json:"truncated"`
}

// Meta is recording header metadata.
type Meta struct {
	SessionID string
	User      string
	Server    string
	Account   string
	Term      string
	Width     int
	Height    int
}

// Writer receives timestamped terminal events.
type Writer interface {
	Input(p []byte) error
	Output(p []byte) error
	Close(ctx context.Context) (Object, error)
	Abort()
}

// Store is a recording storage backend.
type Store interface {
	Create(ctx context.Context, meta Meta) (Writer, error)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Remove(ctx context.Context, key string) error
	Verify(ctx context.Context, key, expectedSHA256 string) error
}

// Filesystem stores recordings under a root directory with 0700/0600 modes.
type Filesystem struct {
	root     string
	maxBytes int64
}

// NewFilesystem creates the root directory if needed.
func NewFilesystem(root string, maxBytes int64) (*Filesystem, error) {
	if root == "" {
		return nil, errors.New("recording: directory is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("recording: create directory: %w", err)
	}
	if maxBytes <= 0 {
		maxBytes = 256 << 20
	}
	return &Filesystem{root: root, maxBytes: maxBytes}, nil
}

// Create implements Store.
func (f *Filesystem) Create(_ context.Context, meta Meta) (Writer, error) {
	now := time.Now().UTC()
	key := filepath.Join(now.Format("2006/01/02"), meta.SessionID+".cast")
	final := filepath.Join(f.root, key)
	if err := os.MkdirAll(filepath.Dir(final), 0o700); err != nil {
		return nil, fmt.Errorf("recording: create directory: %w", err)
	}
	tmp := final + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("recording: create file: %w", err)
	}
	w := &fileWriter{
		file:     file,
		tmp:      tmp,
		final:    final,
		key:      key,
		maxBytes: f.maxBytes,
		hash:     sha256.New(),
		start:    now,
	}
	header := map[string]any{
		"version":   2,
		"width":     meta.Width,
		"height":    meta.Height,
		"timestamp": now.Unix(),
		"title":     fmt.Sprintf("%s@%s via RSCON", meta.Account, meta.Server),
		"env":       map[string]string{"TERM": meta.Term, "RSCON_USER": meta.User, "RSCON_SESSION": meta.SessionID},
	}
	line, err := json.Marshal(header)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return nil, err
	}
	if err := w.write(append(line, '\n')); err != nil {
		w.Abort()
		return nil, err
	}
	return w, nil
}

// Open implements Store.
func (f *Filesystem) Open(_ context.Context, key string) (io.ReadCloser, error) {
	clean := filepath.Clean(key)
	if filepath.IsAbs(clean) || clean == ".." || len(clean) >= 3 && clean[:3] == ".."+string(filepath.Separator) {
		return nil, errors.New("recording: invalid object key")
	}
	file, err := os.Open(filepath.Join(f.root, clean))
	if err != nil {
		return nil, fmt.Errorf("recording: open object: %w", err)
	}
	return file, nil
}

// Remove implements Store.
func (f *Filesystem) Remove(_ context.Context, key string) error {
	clean := filepath.Clean(key)
	if filepath.IsAbs(clean) || clean == ".." || len(clean) >= 3 && clean[:3] == ".."+string(filepath.Separator) {
		return errors.New("recording: invalid object key")
	}
	err := os.Remove(filepath.Join(f.root, clean))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Verify recomputes the SHA-256 of a stored object.
func (f *Filesystem) Verify(ctx context.Context, key, expected string) error {
	reader, err := f.Open(ctx, key)
	if err != nil {
		return err
	}
	defer reader.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(reader, f.maxBytes+1)); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != expected {
		return fmt.Errorf("recording: integrity check failed for %s", key)
	}
	return nil
}

type fileWriter struct {
	mu        sync.Mutex
	file      *os.File
	tmp       string
	final     string
	key       string
	maxBytes  int64
	written   int64
	hash      hash.Hash
	start     time.Time
	closed    bool
	truncated bool
	limitWarn bool
}

func (w *fileWriter) write(p []byte) error {
	n, err := w.file.Write(p)
	if n > 0 {
		w.written += int64(n)
		w.hash.Write(p[:n])
	}
	if err != nil {
		return fmt.Errorf("recording: write: %w", err)
	}
	if w.written > w.maxBytes {
		w.truncated = true
		return ErrLimit
	}
	return nil
}

func (w *fileWriter) event(kind string, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.truncated {
		return ErrLimit
	}
	payload, err := json.Marshal(string(data))
	if err != nil {
		return err
	}
	elapsed := time.Since(w.start).Seconds()
	line := fmt.Sprintf("[%.6f, %q, %s]\n", elapsed, kind, payload)
	if err := w.write([]byte(line)); err != nil {
		if errors.Is(err, ErrLimit) {
			w.truncated = true
		}
		return err
	}
	return nil
}

// Input records terminal input; Output records terminal output.
func (w *fileWriter) Input(p []byte) error  { return w.event("i", p) }
func (w *fileWriter) Output(p []byte) error { return w.event("o", p) }

// Close finalises the object.
func (w *fileWriter) Close(_ context.Context) (Object, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return Object{}, errors.New("recording: writer already closed")
	}
	w.closed = true
	if err := w.file.Sync(); err != nil {
		_ = w.file.Close()
		_ = os.Remove(w.tmp)
		return Object{}, fmt.Errorf("recording: sync: %w", err)
	}
	if err := w.file.Close(); err != nil {
		_ = os.Remove(w.tmp)
		return Object{}, fmt.Errorf("recording: close: %w", err)
	}
	if err := os.Rename(w.tmp, w.final); err != nil {
		_ = os.Remove(w.tmp)
		return Object{}, fmt.Errorf("recording: finalise: %w", err)
	}
	return Object{
		Key:       w.key,
		Backend:   "filesystem",
		Format:    "asciicast-v2",
		Size:      w.written,
		SHA256:    hex.EncodeToString(w.hash.Sum(nil)),
		Truncated: w.truncated,
	}, nil
}

// Abort discards a partial recording.
func (w *fileWriter) Abort() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	_ = w.file.Close()
	_ = os.Remove(w.tmp)
}
