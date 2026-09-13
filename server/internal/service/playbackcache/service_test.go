package playbackcache

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/remux"
)

// blockingRunner lets shutdown tests wait until cancellation reaches concat.
type blockingRunner struct {
	started chan struct{}
	once    sync.Once
	ctxErr  error
}

func (r *blockingRunner) Concat(ctx context.Context, _, _ string, _ remux.FileOperations) error {
	r.once.Do(func() { close(r.started) })
	<-ctx.Done()
	r.ctxErr = ctx.Err()
	return ctx.Err()
}

func TestCloseCancelsInflightBuild(t *testing.T) {
	svc, _, _, _ := cacheFixture(t, nil)
	runner := &blockingRunner{started: make(chan struct{})}
	svc.SetRunner(runner)
	if err := svc.StartBuild(t.Context(), 42); err != nil {
		t.Fatal(err)
	}

	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("build never reached the runner")
	}

	done := make(chan struct{})
	go func() { svc.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return; in-flight build was not canceled")
	}
	if runner.ctxErr == nil {
		t.Fatal("runner was not canceled by Close")
	}
}

// TestReconcileDoesNotBuildBacklog checks that pruning cannot trigger bulk
// playback generation.
func TestReconcileDoesNotBuildBacklog(t *testing.T) {
	ctx := t.Context()
	svc, repo, store, runner := cacheFixture(t, nil)

	if err := svc.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	svc.Wait() // join anything the reconciler might have started (it must not)

	if runner.calls != 0 {
		t.Fatalf("concat calls = %d, want 0 (Reconcile must not build)", runner.calls)
	}
	if repo.asset != nil {
		t.Fatalf("asset = %#v, want nil (no build on reconcile)", repo.asset)
	}
	assertPlaybackFiles(t, svc, store)
}
