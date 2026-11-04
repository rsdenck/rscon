package recording

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestFilesystemRecording(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFilesystem(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	w, err := store.Create(context.Background(), Meta{
		SessionID: "sess-1", User: "alice", Server: "web01", Account: "deploy",
		Term: "xterm-256color", Width: 120, Height: 40,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Output([]byte("hello ")); err != nil {
		t.Fatal(err)
	}
	if err := w.Input([]byte("uptime\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Output([]byte("world\n")); err != nil {
		t.Fatal(err)
	}
	obj, err := w.Close(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if obj.Size == 0 || obj.SHA256 == "" || obj.Truncated {
		t.Fatalf("unexpected object: %+v", obj)
	}
	if err := store.Verify(context.Background(), obj.Key, obj.SHA256); err != nil {
		t.Fatalf("Verify() = %v", err)
	}
	reader, err := store.Open(context.Background(), obj.Key)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(reader)
	reader.Close()
	text := string(data)
	if !strings.Contains(text, `"version":2`) || !strings.Contains(text, `[`) {
		t.Fatalf("unexpected asciicast content: %s", text)
	}
	if !strings.Contains(text, `"i"`) || !strings.Contains(text, `"o"`) {
		t.Fatalf("expected input and output events: %s", text)
	}
	if err := store.Verify(context.Background(), obj.Key, strings.Repeat("0", 64)); err == nil {
		t.Fatal("expected integrity failure")
	}
}

func TestRecordingLimit(t *testing.T) {
	store, err := NewFilesystem(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	w, err := store.Create(context.Background(), Meta{SessionID: "s2", Width: 80, Height: 24})
	if err != nil {
		t.Fatal(err)
	}
	var writeErr error
	for i := 0; i < 100; i++ {
		if err := w.Output([]byte(strings.Repeat("x", 100))); err != nil {
			writeErr = err
			break
		}
	}
	if writeErr != ErrLimit {
		t.Fatalf("expected ErrLimit, got %v", writeErr)
	}
	obj, err := w.Close(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !obj.Truncated {
		t.Fatal("expected truncated object")
	}
}

func TestRejectPathTraversal(t *testing.T) {
	store, _ := NewFilesystem(t.TempDir(), 1024)
	if _, err := store.Open(context.Background(), "../../etc/passwd"); err == nil {
		t.Fatal("expected path traversal rejection")
	}
}
