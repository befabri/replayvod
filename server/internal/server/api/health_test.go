package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/service/storagehealth"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func TestHealthHandler_OK(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rr := httptest.NewRecorder()
	healthHandler(fakePinger{}, nil, log)(rr, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status field = %q, want %q", body["status"], "ok")
	}
}

type healthBlockedRoot struct {
	*storage.LocalStorage
	block   atomic.Bool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *healthBlockedRoot) ProbeRoot(context.Context) error {
	if s.block.Load() {
		s.once.Do(func() { close(s.entered) })
		<-s.release
	}
	return nil
}

// TestHealthHandlerRespondsDuringStuckProbeAndRejectsExpiredSuccess runs in a
// bubble, whose clock advances only once every goroutine is durably blocked, so
// slow setup I/O can neither exhaust the probe deadline nor expire the verdict
// before the first request.
func TestHealthHandlerRespondsDuringStuckProbeAndRejectsExpiredSuccess(t *testing.T) {
	synctest.Test(t, testHealthHandlerRespondsDuringStuckProbeAndRejectsExpiredSuccess)
}

func testHealthHandlerRespondsDuringStuckProbeAndRejectsExpiredSuccess(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	local, err := storage.NewLocal(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	store := &healthBlockedRoot{LocalStorage: local, entered: make(chan struct{}), release: make(chan struct{})}
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	mon := storagehealth.New(repo, store, nil, log, "local", local.Root,
		storagehealth.WithInterval(20*time.Millisecond), storagehealth.WithProbeTimeout(200*time.Millisecond))
	status, err := mon.Attach(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	store.block.Store(true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { mon.Check(ctx); close(done) }()
	t.Cleanup(func() {
		close(store.release)
		cancel()
		<-done
		// Wait for the old worker to finish before the fixture is removed.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
		defer cleanupCancel()
		_ = mon.Verify(cleanupCtx)
	})
	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	handler := healthHandler(fakePinger{}, mon, log)
	request := func(want int) {
		t.Helper()
		answered := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			rr := httptest.NewRecorder()
			handler(rr, httptest.NewRequest(http.MethodGet, "/health", nil))
			answered <- rr
		}()
		select {
		case rr := <-answered:
			if rr.Code != want {
				t.Fatalf("health = %d %s, want %d", rr.Code, rr.Body.String(), want)
			}
			if strings.Contains(rr.Body.String(), local.Root) {
				t.Fatal("health exposed the storage path")
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("health waited for a storage probe")
		}
	}
	request(http.StatusOK)
	// Abandon the request that started the probe. Its worker remains blocked;
	// expiry must work even without a background check publishing a timeout.
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled check did not return")
	}
	<-time.After(time.Until(status.CheckedAt.Add(250 * time.Millisecond)))
	request(http.StatusServiceUnavailable)
}

func TestHealthHandler_UnhealthyDoesNotLeakError(t *testing.T) {
	const secret = "dsn=postgres://user:hunter2@db:5432 connection refused"
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rr := httptest.NewRecorder()
	healthHandler(fakePinger{err: errors.New(secret)}, nil, log)(rr, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
	if strings.Contains(rr.Body.String(), "hunter2") || strings.Contains(rr.Body.String(), "connection refused") {
		t.Fatalf("response body leaked the raw DB error: %s", rr.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "unhealthy" {
		t.Fatalf("status field = %q, want %q", body["status"], "unhealthy")
	}
	if _, ok := body["error"]; ok {
		t.Fatalf("response body must not include an error field, got: %v", body)
	}
}

type fakeStorageReadiness struct{ err error }

func (f fakeStorageReadiness) Ready() error { return f.err }

func TestHealthHandler_StorageVerdict(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cases := []struct {
		name        string
		err         error
		wantCode    int
		wantStorage string
	}{
		{name: "attached", err: nil, wantCode: http.StatusOK, wantStorage: "attached"},
		{name: "full still serves", err: storage.ErrFull, wantCode: http.StatusOK, wantStorage: "attached"},
		{name: "read-only still serves", err: fmt.Errorf("%w: /mnt/data", storage.ErrReadOnly), wantCode: http.StatusOK, wantStorage: "attached"},
		{name: "unattached", err: fmt.Errorf("%w: marker missing at /mnt/data", storage.ErrUnattached), wantCode: http.StatusServiceUnavailable, wantStorage: "unattached"},
		{name: "unreachable", err: fmt.Errorf("%w: stat /mnt/data", storage.ErrUnreachable), wantCode: http.StatusServiceUnavailable, wantStorage: "unattached"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			healthHandler(fakePinger{}, fakeStorageReadiness{err: tc.err}, log)(rr, httptest.NewRequest(http.MethodGet, "/health", nil))
			if rr.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rr.Code, tc.wantCode)
			}
			var body map[string]string
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["storage"] != tc.wantStorage || body["status"] != "ok" {
				t.Fatalf("body = %v, want storage %q and status ok", body, tc.wantStorage)
			}
			if strings.Contains(rr.Body.String(), "/mnt/data") {
				t.Fatalf("response leaked the storage location: %s", rr.Body.String())
			}
		})
	}
}
