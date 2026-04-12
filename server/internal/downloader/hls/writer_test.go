package hls

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPartWriter_CommitAtomicRename(t *testing.T) {
	dir := t.TempDir()
	w, err := NewPartWriter(dir, "42.ts")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := io.Copy(w, strings.NewReader("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Before commit: .part exists, final doesn't.
	assertExists(t, filepath.Join(dir, "42.ts.part"))
	assertNotExists(t, filepath.Join(dir, "42.ts"))

	if err := w.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	// After commit: .part gone, final present.
	assertNotExists(t, filepath.Join(dir, "42.ts.part"))
	assertExists(t, filepath.Join(dir, "42.ts"))

	if got, want := w.BytesWritten(), int64(5); got != want {
		t.Errorf("BytesWritten=%d, want %d", got, want)
	}

	body, err := os.ReadFile(filepath.Join(dir, "42.ts"))
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	if string(body) != "hello" {
		t.Errorf("body=%q, want hello", body)
	}
}

func TestPartWriter_AbortRemovesPart(t *testing.T) {
	dir := t.TempDir()
	w, err := NewPartWriter(dir, "99.ts")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := io.Copy(w, strings.NewReader("partial")); err != nil {
		t.Fatalf("write: %v", err)
	}
	w.Abort()
	assertNotExists(t, filepath.Join(dir, "99.ts.part"))
	assertNotExists(t, filepath.Join(dir, "99.ts"))
}

func TestPartWriter_AbortIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	w, err := NewPartWriter(dir, "1.ts")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	w.Abort()
	w.Abort() // second call must not panic
}

func TestPartWriter_AbortAfterCommitIsNoop(t *testing.T) {
	dir := t.TempDir()
	w, err := NewPartWriter(dir, "7.ts")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := io.Copy(w, strings.NewReader("done")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	w.Abort() // no-op; must not remove the committed file
	assertExists(t, filepath.Join(dir, "7.ts"))
}

func TestPartWriter_WriteAfterCommitErrors(t *testing.T) {
	dir := t.TempDir()
	w, err := NewPartWriter(dir, "5.ts")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := io.Copy(w, strings.NewReader("x")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, err := w.Write([]byte("late")); !errors.Is(err, ErrWriterClosed) {
		t.Errorf("Write after Commit err=%v, want ErrWriterClosed", err)
	}
}

func TestPartWriter_RejectStalePartFile(t *testing.T) {
	dir := t.TempDir()
	// Pre-create a stale .part from a prior crash.
	stalePath := filepath.Join(dir, "10.ts.part")
	if err := os.WriteFile(stalePath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := NewPartWriter(dir, "10.ts")
	if err == nil {
		t.Fatal("expected error when .part already exists")
	}
	if !os.IsExist(errors.Unwrap(err)) && !strings.Contains(err.Error(), "exist") {
		t.Errorf("err=%v, want os.IsExist-style", err)
	}
}

func TestPartWriter_ReadFromFastPath(t *testing.T) {
	// os.File implements ReaderFrom in Go 1.15+, so PartWriter.ReadFrom
	// should copy the source without our bufPool. Behavior-level check:
	// the final file contents match.
	dir := t.TempDir()
	w, err := NewPartWriter(dir, "rf.ts")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	n, err := w.ReadFrom(strings.NewReader("read-from-source"))
	if err != nil {
		t.Fatalf("readfrom: %v", err)
	}
	if n != int64(len("read-from-source")) {
		t.Errorf("n=%d, want %d", n, len("read-from-source"))
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "rf.ts"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "read-from-source" {
		t.Errorf("body=%q", body)
	}
}

func TestPartWriter_EmptyFinalNameErrors(t *testing.T) {
	_, err := NewPartWriter(t.TempDir(), "")
	if err == nil {
		t.Fatal("expected error for empty finalName")
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to exist: %v", path, err)
	}
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s not to exist, err=%v", path, err)
	}
}
