package video

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// blockingMarker records MarkMissing calls and holds each one until released,
// so a test can observe callers waiting on the running check.
type blockingMarker struct {
	mu         sync.Mutex
	calls      []int64
	release    chan struct{}
	tombstoned bool
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
