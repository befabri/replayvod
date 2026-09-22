package video

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/service/storagehealth"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// blockingMarker records MarkMissing calls and holds each one until released,
// so a test can observe callers waiting on the running check.
type blockingMarker struct {
	mu         sync.Mutex
	calls      []int64
	release    chan struct{}
	tombstoned bool
}

type streamBlockedRoot struct {
	*storage.LocalStorage
	block   atomic.Bool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *streamBlockedRoot) ProbeRoot(context.Context) error {
	if s.block.Load() {
		s.once.Do(func() { close(s.entered) })
		<-s.release
	}
	return nil
}

func TestStreamPart_StuckProbeAnswers503WithoutTombstoning(t *testing.T) {
	local, err := storage.NewLocal(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	store := &streamBlockedRoot{LocalStorage: local, entered: make(chan struct{}), release: make(chan struct{})}
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	mon := storagehealth.New(repo, store, nil, testClientLogger(), "local", local.Root)
	if _, err := mon.Attach(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(t.Context(), storage.MarkerPath); err != nil {
		t.Fatal(err)
	}
	if mon.Check(t.Context()).State != storagehealth.StateUnattached {
		t.Fatal("missing marker did not make storage unattached")
	}
	store.block.Store(true)
	checked := make(chan struct{})
	go func() { mon.Check(t.Context()); close(checked) }()
	release := sync.OnceFunc(func() { close(store.release) })
	t.Cleanup(func() {
		release()
		<-checked
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = mon.Verify(ctx)
	})
	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	marker := newBlockingMarker(true)
	close(marker.release)
	srv := streamRouteTestServer(t, missingPartRepo(), &signedStorage{bodies: map[string][]byte{}}, testClientLogger(), WithMissingMarker(marker), WithStorageGate(mon))
	// Release the probe before the HTTP server cleanup if this test catches a
	// regression that strands an HTTP request inside Ready.
	t.Cleanup(release)
	done := getSessionPartAsync(t, srv.URL, 7, 1)
	select {
	case status := <-done:
		if status != http.StatusServiceUnavailable || marker.callCount() != 0 {
			t.Fatalf("stream = %d, MarkMissing calls = %d", status, marker.callCount())
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("stream request waited for the readiness probe")
	}
}

func newBlockingMarker(tombstoned bool) *blockingMarker {
	return &blockingMarker{release: make(chan struct{}), tombstoned: tombstoned}
}

func (m *blockingMarker) MarkMissing(ctx context.Context, videoID int64) (bool, error) {
	m.mu.Lock()
	m.calls = append(m.calls, videoID)
	m.mu.Unlock()
	select {
	case <-m.release:
	case <-ctx.Done():
	}
	return m.tombstoned, nil
}

func (m *blockingMarker) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func waitForCalls(t *testing.T, m *blockingMarker, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m.callCount() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("MarkMissing calls = %d, want %d", m.callCount(), want)
}

func missingPartRepo() *signedRepo {
	return &signedRepo{
		video: &repository.Video{ID: 7, Status: repository.VideoStatusDone, Filename: "rec"},
		parts: []repository.VideoPart{{PartIndex: 1, Filename: "rec-part01.mp4"}},
	}
}

// getSessionPartAsync issues the part request on its own goroutine and hands
// back the status once the handler answers.
func getSessionPartAsync(t *testing.T, srv string, videoID int64, part int32) <-chan int {
	t.Helper()
	done := make(chan int, 1)
	go func() {
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/videos/%d/parts/%d/stream", srv, videoID, part))
		if err != nil {
			done <- -1
			return
		}
		resp.Body.Close()
		done <- resp.StatusCode
	}()
	return done
}

func TestStreamPart_MissingFileAnswers404AfterOneCheck(t *testing.T) {
	marker := newBlockingMarker(true)
	// An empty bodies map makes every Stat report not-found.
	store := &signedStorage{bodies: map[string][]byte{}}
	srv := streamRouteTestServer(t, missingPartRepo(), store, testClientLogger(), WithMissingMarker(marker))

	first := getSessionPartAsync(t, srv.URL, 7, 1)
	waitForCalls(t, marker, 1)
	second := getSessionPartAsync(t, srv.URL, 7, 1)

	// Neither request is answered while the check runs: the 404 must land
	// after the tombstone so a refetch sees it.
	select {
	case status := <-first:
		t.Fatalf("first request answered %d before the check finished", status)
	case status := <-second:
		t.Fatalf("second request answered %d before the check finished", status)
	case <-time.After(50 * time.Millisecond):
	}
	close(marker.release)

	for _, done := range []<-chan int{first, second} {
		select {
		case status := <-done:
			if status != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 for a missing part", status)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("request did not finish after the check was released")
		}
	}
	if n := marker.callCount(); n != 1 {
		t.Fatalf("MarkMissing calls = %d, want 1 (the waiter reuses the finished check)", n)
	}
	if got := marker.calls[0]; got != 7 {
		t.Fatalf("MarkMissing video id = %d, want 7", got)
	}
}

func TestStreamPart_PartialRecordingIsCheckedOncePerCooldown(t *testing.T) {
	marker := newBlockingMarker(false)
	close(marker.release)
	store := &signedStorage{bodies: map[string][]byte{}}
	srv := streamRouteTestServer(t, missingPartRepo(), store, testClientLogger(), WithMissingMarker(marker))

	for range 3 {
		resp := getSessionPart(t, srv, 7, 1)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	}
	if n := marker.callCount(); n != 1 {
		t.Fatalf("MarkMissing calls = %d, want 1 within the cooldown", n)
	}
}

func TestStreamPart_StorageErrorAnswers503WithoutMarking(t *testing.T) {
	marker := newBlockingMarker(false)
	close(marker.release)
	store := &signedStorage{
		body:     []byte("video-bytes"),
		statErrs: map[string][]error{"videos/rec-part01.mp4": {errors.New("storage hiccup")}},
	}
	srv := streamRouteTestServer(t, missingPartRepo(), store, testClientLogger(), WithMissingMarker(marker))

	resp := getSessionPart(t, srv, 7, 1)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 for a storage error", resp.StatusCode)
	}
	if n := marker.callCount(); n != 0 {
		t.Fatalf("MarkMissing calls = %d, want 0 for a non-not-found error", n)
	}
}

func TestStreamPart_MissingFileWithoutMarkerStillAnswers404(t *testing.T) {
	store := &signedStorage{bodies: map[string][]byte{}}
	srv := streamRouteTestServer(t, missingPartRepo(), store, testClientLogger())

	resp := getSessionPart(t, srv, 7, 1)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestMissingCheck_CancelledWaiterReturnsPromptly(t *testing.T) {
	marker := newBlockingMarker(false)
	h := &StreamHandler{missing: marker}
	owner := make(chan error, 1)
	go func() { owner <- h.markMissing(t.Context(), 7) }()
	waitForCalls(t, marker, 1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := h.markMissing(ctx, 7); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter = %v", err)
	}
	close(marker.release)
	if err := <-owner; err != nil {
		t.Fatal(err)
	}
}

type markerFunc func(context.Context, int64) (bool, error)

func (f markerFunc) MarkMissing(ctx context.Context, id int64) (bool, error) { return f(ctx, id) }

func TestMissingCheck_DoesNotCacheFailures(t *testing.T) {
	calls := 0
	h := &StreamHandler{missing: markerFunc(func(context.Context, int64) (bool, error) {
		calls++
		if calls == 1 {
			return false, errors.New("temporary outage")
		}
		return false, nil
	})}
	if err := h.markMissing(t.Context(), 7); err == nil {
		t.Fatal("lost error")
	}
	if err := h.markMissing(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestMissingCheck_BoundsCacheAndEvicts(t *testing.T) {
	calls := 0
	h := &StreamHandler{missing: markerFunc(func(context.Context, int64) (bool, error) { calls++; return false, nil })}
	for id := int64(1); id <= maxMissingChecks+1; id++ {
		if err := h.markMissing(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.missingChecks) != maxMissingChecks {
		t.Fatalf("cache size = %d", len(h.missingChecks))
	}
	if err := h.markMissing(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if calls != maxMissingChecks+2 {
		t.Fatalf("oldest cached result not evicted; calls=%d", calls)
	}
}

func TestMissingCheck_BoundsConcurrentWork(t *testing.T) {
	marker := newBlockingMarker(false)
	h := &StreamHandler{missing: marker}
	done := make(chan error, maxActiveMissingChecks)
	for id := int64(1); id <= maxActiveMissingChecks; id++ {
		go func() { done <- h.markMissing(t.Context(), id) }()
	}
	waitForCalls(t, marker, maxActiveMissingChecks)
	if err := h.markMissing(t.Context(), 99); err == nil {
		t.Fatal("accepted work beyond concurrency limit")
	}
	close(marker.release)
	for range maxActiveMissingChecks {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestStreamPart_InconclusiveMissingCheckAnswers503(t *testing.T) {
	store := &signedStorage{bodies: map[string][]byte{}}
	marker := markerFunc(func(context.Context, int64) (bool, error) { return false, errors.New("root unavailable") })
	srv := streamRouteTestServer(t, missingPartRepo(), store, testClientLogger(), WithMissingMarker(marker))
	resp := getSessionPart(t, srv, 7, 1)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

type gateFunc func() error

func (f gateFunc) Ready() error { return f() }

func (f gateFunc) Verify(context.Context) error { return f() }

// TestStreamPart_UnattachedStorageAnswers503WithoutMarking pins that an absent
// file on storage that is not attached is an outage, never a tombstone: the
// player gets a retryable 503 and the missing marker is never consulted.
func TestStreamPart_UnattachedStorageAnswers503WithoutMarking(t *testing.T) {
	cases := []struct {
		name       string
		gate       error
		wantStatus int
		wantCalls  int
	}{
		{name: "unattached", gate: fmt.Errorf("%w: marker missing", storage.ErrUnattached), wantStatus: http.StatusServiceUnavailable, wantCalls: 0},
		{name: "unreachable", gate: fmt.Errorf("%w: stat", storage.ErrUnreachable), wantStatus: http.StatusServiceUnavailable, wantCalls: 0},
		{name: "read-only still reconciles", gate: fmt.Errorf("%w: probe", storage.ErrReadOnly), wantStatus: http.StatusNotFound, wantCalls: 1},
		{name: "attached", gate: nil, wantStatus: http.StatusNotFound, wantCalls: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			marker := newBlockingMarker(true)
			close(marker.release)
			store := &signedStorage{bodies: map[string][]byte{}}
			gate := gateFunc(func() error { return tc.gate })
			srv := streamRouteTestServer(t, missingPartRepo(), store, testClientLogger(), WithMissingMarker(marker), WithStorageGate(gate))
			resp, err := http.Get(fmt.Sprintf("%s/api/v1/videos/7/parts/1/stream", srv.URL))
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if n := marker.callCount(); n != tc.wantCalls {
				t.Fatalf("MarkMissing calls = %d, want %d", n, tc.wantCalls)
			}
		})
	}
}
