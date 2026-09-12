package storagescan

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/storage"
)

type stalledStats struct {
	storage.Storage
	release chan struct{}
	done    chan struct{}
	calls   atomic.Int32
}

func (s *stalledStats) Stat(ctx context.Context, path string) (storage.FileInfo, error) {
	s.calls.Add(1)
	<-s.release // Like a filesystem syscall, this deliberately ignores ctx.
	defer func() { s.done <- struct{}{} }()
	return s.Storage.Stat(context.WithoutCancel(ctx), path)
}

func TestMissingChecksBoundStalledFilesystemProbes(t *testing.T) {
	f := newFixture(t)
	v := f.seed(t, "stalled-recording", 1)
	store := &stalledStats{Storage: f.store, release: make(chan struct{}), done: make(chan struct{}, scanWorkers)}
	var release sync.Once
	unblock := func() { release.Do(func() { close(store.release) }) }
	t.Cleanup(unblock)
	svc := New(f.repo, store, f.mon, discardLog())
	// Repeated timed-out requests must return without creating unbounded I/O
	// workers. Once every global probe slot is stuck, new callers just wait.
	for range scanWorkers + 2 {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
		done := make(chan error, 1)
		go func() {
			_, err := svc.MarkMissing(ctx, v.ID)
			done <- err
		}()
		select {
		case err := <-done:
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("stalled probe returned %v", err)
			}
		case <-time.After(time.Second):
			cancel()
			unblock()
			<-done
			t.Fatal("missing check waited for a filesystem operation after its deadline")
		}
	}
	if got := store.calls.Load(); got != scanWorkers {
		t.Fatalf("stalled filesystem workers = %d, want %d", got, scanWorkers)
	}
	unblock()
	for range scanWorkers {
		select {
		case <-store.done:
		case <-time.After(time.Second):
			t.Fatal("unblocked filesystem worker did not finish")
		}
	}
	f.assertLive(t, v.ID)
}
