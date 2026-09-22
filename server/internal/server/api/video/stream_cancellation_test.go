package video

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/service/storagehealth"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/videodownload"
	"github.com/go-chi/chi/v5"
)

// hungMount blocks the root probe the way a wedged network mount does: the
// syscall ignores cancellation and returns only once the mount recovers.
type hungMount struct {
	*storage.LocalStorage
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func newHungMount(t *testing.T, local *storage.LocalStorage) *hungMount {
	t.Helper()
	m := &hungMount{LocalStorage: local, entered: make(chan struct{}, 16), release: make(chan struct{})}
	t.Cleanup(func() {
		if m.armed.CompareAndSwap(true, false) {
			close(m.release)
		}
	})
	return m
}

func (m *hungMount) arm() { m.armed.Store(true) }

func (m *hungMount) free() {
	if m.armed.CompareAndSwap(true, false) {
		close(m.release)
	}
}

func (m *hungMount) ProbeRoot(ctx context.Context) error {
	if !m.armed.Load() {
		return m.LocalStorage.ProbeRoot(ctx)
	}
	m.entered <- struct{}{}
	<-m.release
	return nil
}

// stalledStorageFixture serves a real recording part behind a real health
// monitor, so the readiness errors under test are the ones production builds
// rather than shapes hand-written by the test.
type stalledStorageFixture struct {
	h     http.Handler
	mount *hungMount
	repo  *signedRepo
	logs  *capturingHandler
	done  chan struct{}
}

func newStalledStorageFixture(t *testing.T, probeTimeout time.Duration, opts ...StreamHandlerOption) *stalledStorageFixture {
	t.Helper()
	local, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := local.Save(t.Context(), storagekeys.Video("rec-part01.mp4"), strings.NewReader("part bytes")); err != nil {
		t.Fatal(err)
	}
	assetName := "playback.mp4"
	if err := local.Save(t.Context(), storagekeys.Video(assetName), strings.NewReader("playback bytes")); err != nil {
		t.Fatal(err)
	}
	repo := missingPartRepo()
	repo.asset = &repository.VideoPlaybackAsset{Status: repository.PlaybackAssetStatusReady, Filename: &assetName}

	mount := newHungMount(t, local)
	logs := &capturingHandler{}
	// The default interval keeps the cached verdict alive for the whole test, so
	// every refusal below comes from the probe rather than from expiry.
	monitor := storagehealth.New(sqliteadapter.New(testdb.NewSQLiteDB(t)), mount, nil,
		slog.New(logs), "local", local.Root, storagehealth.WithProbeTimeout(probeTimeout))
	if _, err := monitor.Attach(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := monitor.Ready(); err != nil {
		t.Fatalf("fixture did not start healthy: %v", err)
	}
	mount.arm()

	done := make(chan struct{}, 4)
	h := NewStreamHandler(repo, streamMedia(t, repo, local, nil, nil), videodownload.NewVerifier(signTestSecret),
		slog.New(logs), append([]StreamHandlerOption{WithStorageGate(monitor)}, opts...)...)
	t.Cleanup(func() {
		mount.free()
		if err := h.Close(); err != nil {
			t.Error(err)
		}
	})
	router := chi.NewRouter()
	router.Route("/api/v1", func(r chi.Router) {
		h.SetupRoutes(r, func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				defer func() { done <- struct{}{} }()
				next.ServeHTTP(w, req)
			})
		})
	})
	return &stalledStorageFixture{h: router, mount: mount, repo: repo, logs: logs, done: done}
}

// get serves in process: a bubble's clock cannot advance while a goroutine
// waits on a real socket.
func (f *stalledStorageFixture) get(ctx context.Context, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1"+path, nil))
	return rec
}

func (f *stalledStorageFixture) awaitHandler(t *testing.T) {
	t.Helper()
	select {
	case <-f.done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never answered")
	}
}

func (f *stalledStorageFixture) awaitProbe(t *testing.T) {
	t.Helper()
	select {
	case <-f.mount.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached the storage probe")
	}
}

// TestStreamPart_ClientAbortIsNotLoggedAsAStorageOutage pins the common case: a
// viewer who seeks or closes the tab cancels the request while the readiness
// probe is still in the backend. That abort is not an outage, and logging it as
// one pages on error volume while saying nothing true about storage.
func TestStreamPart_ClientAbortIsNotLoggedAsAStorageOutage(t *testing.T) {
	synctest.Test(t, testStreamPartClientAbortIsNotLoggedAsAStorageOutage)
}

func testStreamPartClientAbortIsNotLoggedAsAStorageOutage(t *testing.T) {
	marker := newBlockingMarker(true)
	close(marker.release)
	f := newStalledStorageFixture(t, 10*time.Second, WithMissingMarker(marker))

	ctx, cancel := context.WithCancel(t.Context())
	go f.get(ctx, "/videos/7/parts/1/stream")

	f.awaitProbe(t)
	cancel()
	f.mount.free()
	f.awaitHandler(t)

	if n := f.logs.countAtLeast(slog.LevelError); n != 0 {
		t.Fatalf("client abort logged %d error record(s); a departed viewer is not a storage fault", n)
	}
	if marker.callCount() != 0 || f.repo.deletes != 0 {
		t.Fatalf("abort drove reconciliation: MarkMissing=%d deletes=%d", marker.callCount(), f.repo.deletes)
	}
}

// TestStreamPart_StalledStorageKeeps503SoThePlayerRetries is the mirror: a
// stalled backend must not be excused as a departed client, because 499 tells
// the player the request was abandoned and it stops retrying an outage that 503
// would have ridden out.
func TestStreamPart_StalledStorageKeeps503SoThePlayerRetries(t *testing.T) {
	synctest.Test(t, testStreamPartStalledStorageKeeps503SoThePlayerRetries)
}

func testStreamPartStalledStorageKeeps503SoThePlayerRetries(t *testing.T) {
	marker := newBlockingMarker(true)
	close(marker.release)
	f := newStalledStorageFixture(t, 100*time.Millisecond, WithMissingMarker(marker))

	resp := f.get(t.Context(), "/videos/7/parts/1/stream")
	f.awaitHandler(t)

	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 for a connected viewer on stalled storage", resp.Code)
	}
	if n := f.logs.countAtLeast(slog.LevelError); n == 0 {
		t.Fatal("a stalled backend was never surfaced as an error")
	}
	if marker.callCount() != 0 || f.repo.deletes != 0 {
		t.Fatalf("outage drove reconciliation: MarkMissing=%d deletes=%d", marker.callCount(), f.repo.deletes)
	}
}

// TestPlaybackStream_StalledStorageKeeps503SoThePlayerRetries pins the same
// stall on the playback route, which reaches the verdict through its own branch.
func TestPlaybackStream_StalledStorageKeeps503SoThePlayerRetries(t *testing.T) {
	synctest.Test(t, testPlaybackStreamStalledStorageKeeps503SoThePlayerRetries)
}

func testPlaybackStreamStalledStorageKeeps503SoThePlayerRetries(t *testing.T) {
	f := newStalledStorageFixture(t, 100*time.Millisecond)

	resp := f.get(t.Context(), "/videos/7/playback/stream")
	f.awaitHandler(t)

	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 for a connected viewer on stalled storage", resp.Code)
	}
	if f.repo.deletes != 0 {
		t.Fatal("demoted a ready playback asset on a storage stall")
	}
}

// TestClientGone_SeparatesDepartedCallersFromStalledStorage pins the contract
// every 499 in this package rests on, so a new call site can be checked against
// it without reproducing a wedged mount.
func TestClientGone_SeparatesDepartedCallersFromStalledStorage(t *testing.T) {
	live := context.Background()
	dead, cancel := context.WithCancel(context.Background())
	cancel()

	probeDeadline := fmt.Errorf("%w: storage probe did not finish: %w", storage.ErrUnreachable, context.DeadlineExceeded)
	probeAbandoned := fmt.Errorf("%w: %w: %w", storage.ErrUnreachable, storage.ErrCallerGone, context.Canceled)

	for _, tc := range []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{"healthy read", live, nil, false},
		{"stalled storage under a live request", live, probeDeadline, false},
		{"unreachable backend under a live request", live, fmt.Errorf("%w: dial tcp", storage.ErrUnreachable), false},
		{"missing object", live, fs.ErrNotExist, false},
		{"probe abandoned by its caller", live, probeAbandoned, true},
		{"cancelled request", dead, nil, true},
		{"cancelled request on a stalled probe", dead, probeDeadline, true},
		{"cancelled request on a repository read", dead, context.Canceled, true},
		// Neither context error under a live request is the viewer's doing: an
		// internal timer or cancellation fired, and 499 would stop the retry that
		// rides it out.
		{"internal deadline under a live request", live, context.DeadlineExceeded, false},
		{"internal cancellation under a live request", live, context.Canceled, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := clientGone(tc.ctx, tc.err); got != tc.want {
				t.Fatalf("clientGone = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestUnfinishedProbeNeverAuthorizesReads covers both an abandoned probe and a
// stalled backend.
func TestUnfinishedProbeNeverAuthorizesReads(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("%w: %w: %w", storage.ErrUnreachable, storage.ErrCallerGone, context.Canceled),
		fmt.Errorf("%w: storage probe did not finish: %w", storage.ErrUnreachable, context.DeadlineExceeded),
	} {
		if storage.CanRead(err) {
			t.Fatalf("unfinished probe authorized reads: %v", err)
		}
		if !errors.Is(err, storage.ErrUnreachable) {
			t.Fatalf("unfinished probe lost ErrUnreachable: %v", err)
		}
	}
}
