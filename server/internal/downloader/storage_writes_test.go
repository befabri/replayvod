package downloader

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/testutil/mediatest"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
)

func TestSnapshotRefusesUnavailableStorage(t *testing.T) {
	for _, refusal := range []error{storage.ErrUnattached, storage.ErrUnreachable, storage.ErrReadOnly, storage.ErrFull} {
		t.Run(refusal.Error(), func(t *testing.T) {
			store := newFakeStorage()
			writer, _ := snapshotWriterFixture(t, store, "recording")
			mediatest.SetGate(writer.storage, gateFunc(func() error { return refusal }))
			if err := writer.WriteSnapshot(t.Context(), 0, strings.NewReader("frame")); !errors.Is(err, refusal) {
				t.Fatal(err)
			}
			if len(store.saves) != 0 {
				t.Fatal("refused snapshot published")
			}

		})
	}
}

type changingUploadStorage struct {
	storage.Storage
	first      storage.Storage
	afterFirst func()
	calls      int
}

func (s *changingUploadStorage) Save(ctx context.Context, path string, body io.Reader) error {
	s.calls++
	if s.calls == 1 {
		err := s.first.Save(ctx, path, body)
		s.afterFirst()
		return err
	}
	return s.Storage.Save(ctx, path, body)
}

func TestUploadRepeatsAfterStorageChangesDuringSave(t *testing.T) {
	trusted, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lost, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var unavailable atomic.Bool
	refused := make(chan struct{})
	var once sync.Once
	store := &changingUploadStorage{Storage: trusted, first: lost, afterFirst: func() { unavailable.Store(true) }}
	s := newTestService(t, t.TempDir())
	d := seedWebhookAttempt(t, s, "media")
	s.storage = mediatest.New(t, s.repo, store, gateFunc(func() error {
		if unavailable.Load() {
			once.Do(func() { close(refused) })
			return storage.ErrUnattached
		}
		return nil
	}), nil)
	scratch := filepath.Join(t.TempDir(), "media.mp4")
	const body = "complete recording bytes"
	if err := os.WriteFile(scratch, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	var uploadErr error
	go func() { defer close(done); uploadErr = s.uploadFromScratch(ctx, d, scratch, "videos/media.mp4") }()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case <-refused:
	case <-done:
		t.Fatal("upload succeeded without checking storage after Save")
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not observe the outage")
	}
	if exists, err := trusted.Exists(t.Context(), "videos/media.mp4"); err != nil || exists {
		t.Fatalf("trusted target should still be empty: exists=%v err=%v", exists, err)
	}
	unavailable.Store(false)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not resume")
	}
	if uploadErr != nil || store.calls != 2 {
		t.Fatalf("upload recovery: err=%v calls=%d", uploadErr, store.calls)
	}
	path, err := trusted.LocalPath("videos/media.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != body {
		t.Fatalf("retry did not reopen the complete scratch input: %q err=%v", got, err)
	}
}

type storageChangeGenerator struct{ after func() }

func (g storageChangeGenerator) Generate(context.Context, string, float64, int) ([]float32, error) {
	g.after()
	return []float32{0.1, 0.8}, nil
}

func TestRecordingWaveformWaitsForWritableStorage(t *testing.T) {
	store := newFakeStorage()
	var unavailable atomic.Bool
	refused := make(chan struct{})
	var once sync.Once
	s := newTestService(t, t.TempDir())
	d := seedWebhookAttempt(t, s, "media")
	s.storage = mediatest.New(t, s.repo, store, gateFunc(func() error {
		if unavailable.Load() {
			once.Do(func() { close(refused) })
			return storage.ErrReadOnly
		}
		return nil
	}), nil)
	s.waveforms = storageChangeGenerator{after: func() { unavailable.Store(true) }}
	local := filepath.Join(t.TempDir(), "audio.m4a")
	if err := os.WriteFile(local, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	var buildErr error
	go func() {
		defer close(done)
		buildErr = s.persistAudioWaveform(ctx, d, "recording", repository.RecordingTypeAudio, 2, []partResult{{filename: "audio.m4a", localPath: local, durationSeconds: 2, sizeBytes: 5}})
	}()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case <-refused:
	case <-done:
		t.Fatalf("waveform skipped the storage gate: %v", buildErr)
	case <-time.After(5 * time.Second):
		t.Fatal("waveform did not reach publication")
	}
	store.mu.Lock()
	saved := len(store.saves)
	store.mu.Unlock()
	if saved != 0 {
		t.Fatal("waveform written to read-only storage")
	}
	unavailable.Store(false)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("waveform did not resume")
	}
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	key, err := s.repo.GetVideoWaveformKey(t.Context(), d.videoID)
	if err != nil || len(store.saves[key]) == 0 {
		t.Fatal("waveform was lost after storage recovery")
	}
}
