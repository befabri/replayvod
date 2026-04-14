package downloader

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/befabri/replayvod/server/internal/storage"
)

// fakeStorage is a minimal storage.Storage test double — only the
// Save method is exercised by storageSnapshotWriter, so the other
// methods exist as no-ops for interface compliance. Tracks each
// Save call by path so the test can assert the exact path
// template without pulling in the real LocalStorage backend.
type fakeStorage struct {
	mu       sync.Mutex
	saves    map[string][]byte
	saveErrs map[string]error // per-path override; nil = success
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{saves: map[string][]byte{}}
}

func (f *fakeStorage) Save(_ context.Context, path string, r io.Reader) error {
	if err, ok := f.saveErrs[path]; ok && err != nil {
		return err
	}
	buf, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves[path] = buf
	return nil
}

// The other Storage methods are never called by
// storageSnapshotWriter; stubbing satisfies the interface without
// shipping a full fake.
func (f *fakeStorage) Open(_ context.Context, _ string) (io.ReadSeekCloser, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStorage) Delete(_ context.Context, _ string) error { return errors.New("not impl") }
func (f *fakeStorage) Exists(_ context.Context, _ string) (bool, error) {
	return false, errors.New("not impl")
}
func (f *fakeStorage) Stat(_ context.Context, _ string) (storage.FileInfo, error) {
	return storage.FileInfo{}, errors.New("not impl")
}

// TestStorageSnapshotWriter_PathTemplate verifies the path shape
// is exactly what the UI will probe for. The hermetic snapshot
// tests assert URL shape on the CDN-fetch side; this asserts the
// storage-write side. Together they cover the full "hit Twitch →
// store to disk" round trip without needing an integration test
// against real storage + real HTTP.
func TestStorageSnapshotWriter_PathTemplate(t *testing.T) {
	fs := newFakeStorage()
	w := &storageSnapshotWriter{
		storage: fs,
		base:    "thumbnails/20260413-rec-abc12345",
		ctx:     context.Background(),
	}

	// Three writes: snap00, snap01, snap02. The zero-padded
	// index is load-bearing because a UI probing snap01..snap99
	// by convention would otherwise miss single-digit captures
	// on a recording with 10+ snapshots.
	for i := 0; i < 3; i++ {
		if err := w.WriteSnapshot(context.Background(), i, strings.NewReader("snapshot bytes")); err != nil {
			t.Fatalf("WriteSnapshot(%d): %v", i, err)
		}
	}

	wantPaths := []string{
		"thumbnails/20260413-rec-abc12345-snap00.jpg",
		"thumbnails/20260413-rec-abc12345-snap01.jpg",
		"thumbnails/20260413-rec-abc12345-snap02.jpg",
	}
	for _, p := range wantPaths {
		if _, ok := fs.saves[p]; !ok {
			t.Errorf("expected save at %q, got paths: %v", p, keys(fs.saves))
		}
	}
	if len(fs.saves) != len(wantPaths) {
		t.Errorf("saves=%d, want %d", len(fs.saves), len(wantPaths))
	}
}

// TestStorageSnapshotWriter_TwoDigitIndex verifies the zero-
// padding holds up to 99. A 4-hour recording at 5-minute
// intervals produces 48 snapshots — well within range.
func TestStorageSnapshotWriter_TwoDigitIndex(t *testing.T) {
	fs := newFakeStorage()
	w := &storageSnapshotWriter{
		storage: fs,
		base:    "thumbnails/long-rec",
		ctx:     context.Background(),
	}

	for _, idx := range []int{0, 9, 10, 47, 99} {
		if err := w.WriteSnapshot(context.Background(), idx, bytes.NewReader([]byte("x"))); err != nil {
			t.Fatalf("WriteSnapshot(%d): %v", idx, err)
		}
	}
	for _, want := range []string{
		"thumbnails/long-rec-snap00.jpg",
		"thumbnails/long-rec-snap09.jpg",
		"thumbnails/long-rec-snap10.jpg",
		"thumbnails/long-rec-snap47.jpg",
		"thumbnails/long-rec-snap99.jpg",
	} {
		if _, ok := fs.saves[want]; !ok {
			t.Errorf("expected save at %q; saves: %v", want, keys(fs.saves))
		}
	}
}

// TestStorageSnapshotWriter_PropagatesStorageError verifies a
// storage-side failure bubbles out so the Snapshotter's write-
// error handling (log + skip) gets exercised. If the adapter
// swallowed errors the Snapshotter would count failed writes as
// successful captures.
func TestStorageSnapshotWriter_PropagatesStorageError(t *testing.T) {
	wantErr := errors.New("disk full")
	fs := newFakeStorage()
	fs.saveErrs = map[string]error{
		"thumbnails/rec-snap00.jpg": wantErr,
	}
	w := &storageSnapshotWriter{
		storage: fs,
		base:    "thumbnails/rec",
		ctx:     context.Background(),
	}
	err := w.WriteSnapshot(context.Background(), 0, strings.NewReader("bytes"))
	if !errors.Is(err, wantErr) {
		t.Errorf("err=%v, want errors.Is(%v)", err, wantErr)
	}
}

// TestStorageSnapshotWriter_UsesConfiguredCtxNotCall verifies the
// adapter uses its configured ctx (the recording's long-lived
// one), not the per-call ctx (the Snapshotter's derived one
// that's about to cancel). Without this, a snapshot write racing
// the "recording done" signal would be aborted after the CDN
// fetch already succeeded.
func TestStorageSnapshotWriter_UsesConfiguredCtxNotCall(t *testing.T) {
	// Sentinel ctx we'll watch for.
	type ctxKey struct{}
	recordingCtx := context.WithValue(context.Background(), ctxKey{}, "recording")
	// Per-call ctx that's already canceled — simulates the
	// Snapshotter's ctx at teardown.
	callCtx, cancel := context.WithCancel(context.Background())
	cancel()

	var seenVal any
	fs := &ctxInspectingStorage{onSave: func(ctx context.Context) {
		seenVal = ctx.Value(ctxKey{})
	}}
	w := &storageSnapshotWriter{
		storage: fs,
		base:    "thumbnails/rec",
		ctx:     recordingCtx,
	}
	if err := w.WriteSnapshot(callCtx, 0, strings.NewReader("")); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}
	if seenVal != "recording" {
		t.Errorf("Save received ctx.Value(ctxKey)=%v, want %q — adapter should pass the recording ctx, not the per-call ctx", seenVal, "recording")
	}
}

type ctxInspectingStorage struct {
	onSave func(context.Context)
}

func (s *ctxInspectingStorage) Save(ctx context.Context, _ string, r io.Reader) error {
	s.onSave(ctx)
	_, _ = io.Copy(io.Discard, r)
	return nil
}
func (s *ctxInspectingStorage) Open(_ context.Context, _ string) (io.ReadSeekCloser, error) {
	return nil, errors.New("not impl")
}
func (s *ctxInspectingStorage) Delete(_ context.Context, _ string) error {
	return errors.New("not impl")
}
func (s *ctxInspectingStorage) Exists(_ context.Context, _ string) (bool, error) {
	return false, errors.New("not impl")
}
func (s *ctxInspectingStorage) Stat(_ context.Context, _ string) (storage.FileInfo, error) {
	return storage.FileInfo{}, errors.New("not impl")
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
