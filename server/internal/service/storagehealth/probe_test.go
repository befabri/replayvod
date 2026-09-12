package storagehealth

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
)

// stuckRoot deliberately ignores cancellation, like a blocked mount syscall.
// Its late result must not replace the timeout published by the monitor.
type stuckRoot struct {
	*storage.LocalStorage
	entered chan struct{}
	release chan struct{}
	stuck   atomic.Bool
	calls   atomic.Int32
	writes  atomic.Int32
}

func (s *stuckRoot) ProbeRoot(context.Context) error {
	s.calls.Add(1)
	if s.stuck.Load() {
		s.entered <- struct{}{}
		<-s.release
	}
	return nil
}

func (s *stuckRoot) ProbeWrite(context.Context) error { return nil }

func (s *stuckRoot) Save(ctx context.Context, key string, r io.Reader) error {
	s.writes.Add(1)
	return s.LocalStorage.Save(ctx, key, r)
}

func blockRoot(t *testing.T, f fixture) (*stuckRoot, func()) {
	t.Helper()
	s := &stuckRoot{LocalStorage: f.store, entered: make(chan struct{}, 16), release: make(chan struct{})}
	s.stuck.Store(true)
	f.mon.store = s
	release := sync.OnceFunc(func() {
		s.stuck.Store(false)
		close(s.release)
	})
	t.Cleanup(func() {
		release()
		waitProbeIdle(t, f.mon)
	})
	return s, release
}

func receiveProbe[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(time.Second):
		t.Fatal("operation did not return promptly")
		var zero T
		return zero
	}
}

func waitProbeIdle(t *testing.T, m *Monitor) {
	t.Helper()
	select {
	case m.probeGate <- struct{}{}:
		<-m.probeGate
	case <-time.After(time.Second):
		t.Fatal("probe worker did not finish")
	}
}

type readOnlyProbe struct{ *storage.LocalStorage }

func (s readOnlyProbe) ProbeWrite(context.Context) error { return fs.ErrPermission }

func TestCachedReadersAndTimeoutStayResponsiveWithStuckProbe(t *testing.T) {
	f := newFixture(t)
	if _, err := f.mon.Attach(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.mon.probeTimeout = 150 * time.Millisecond
	s, release := blockRoot(t, f)
	events := f.bus.StorageStatus.Subscribe(t.Context())
	done := make(chan Status, 1)
	go func() { done <- f.mon.Check(f.ctx) }()
	receiveProbe(t, s.entered)

	reads := make(chan error, 1)
	go func() {
		_ = f.mon.Status()
		reads <- f.mon.Ready()
	}()
	if err := receiveProbe(t, reads); err != nil {
		t.Fatalf("unexpired cached verdict during probe = %v", err)
	}
	if got := receiveProbe(t, done); got.State != StateUnreachable {
		t.Fatalf("timed-out check = %+v", got)
	}
	if err := f.mon.Ready(); !errors.Is(err, storage.ErrUnreachable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cached timeout = %v", err)
	}
	if got := receiveProbe(t, events); got.State != string(StateUnreachable) {
		t.Fatalf("timeout transition = %+v", got)
	}
	if n := f.eventCount(t, EventUnattached); n != 1 {
		t.Fatalf("timeout rows = %d, want 1", n)
	}

	// Concurrent callers time out waiting; none starts another I/O worker or
	// queues a deferred adopt that would run after its request has ended.
	errs := make(chan error, 8)
	for i := range 8 {
		go func() {
			ctx, cancel := context.WithTimeout(f.ctx, 20*time.Millisecond)
			defer cancel()
			if i%2 == 0 {
				errs <- f.mon.Verify(ctx)
			} else {
				_, err := f.mon.Adopt(ctx, "owner")
				errs <- err
			}
		}()
	}
	for range 8 {
		if err := receiveProbe(t, errs); !errors.Is(err, storage.ErrUnreachable) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("waiting caller = %v", err)
		}
	}
	if got := s.calls.Load(); got != 1 {
		t.Fatalf("started %d probes while one was stuck", got)
	}
	release()
	waitProbeIdle(t, f.mon)
	if f.mon.Status().State != StateUnreachable || f.mon.Ready() == nil {
		t.Fatal("late success overwrote the timeout")
	}
	if s.writes.Load() != 0 {
		t.Fatal("a cancelled adopt wrote a marker")
	}
	if n := f.eventCount(t, EventUnattached); n != 1 {
		t.Fatalf("waiting callers emitted %d outage rows, want 1", n)
	}
	if got := f.mon.Check(f.ctx); got.State != StateAttached {
		t.Fatalf("fresh probe after recovery = %+v", got)
	}
	if got := receiveProbe(t, events); got.State != string(StateAttached) {
		t.Fatalf("recovery transition = %+v", got)
	}
}

func TestReadableCacheExpiresWithoutAnotherProbe(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "attached", true: "read-only"}[readOnly], func(t *testing.T) {
			f := newFixture(t)
			if _, err := f.mon.Attach(f.ctx); err != nil {
				t.Fatal(err)
			}
			waitProbeIdle(t, f.mon)
			if readOnly {
				f.mon.store = readOnlyProbe{f.store}
				if got := f.mon.Check(f.ctx); got.State != StateReadOnly || !errors.Is(f.mon.Ready(), storage.ErrReadOnly) {
					t.Fatalf("read-only probe = %+v, %v", got, f.mon.Ready())
				}
			}
			v := *f.mon.cache.Load()
			if !f.mon.Status().Readable() || !errors.Is(f.mon.Ready(), v.err) {
				t.Fatal("unexpired cache did not retain its verdict")
			}
			// Move only the lease into the past; no successful check occurred.
			expired := v
			expired.expires = time.Now().Add(-time.Second)
			f.mon.cache.Store(&expired)
			got := f.mon.Status()
			if got.Readable() || got.State != StateUnreachable || !errors.Is(f.mon.Ready(), storage.ErrUnreachable) {
				t.Fatalf("expired cache = %+v, %v", got, f.mon.Ready())
			}
			if !got.CheckedAt.Equal(v.status.CheckedAt) || got.StorageID != v.status.StorageID || !strings.Contains(got.Reason, "expired") {
				t.Fatalf("expiry lost the last check's identity/time: %+v", got)
			}
			if got := f.mon.Check(f.ctx); !got.Readable() {
				t.Fatalf("fresh check did not renew lease: %+v", got)
			}
		})
	}
}

func TestCancelledVerifyDoesNotReuseCachedSuccess(t *testing.T) {
	f := newFixture(t)
	if _, err := f.mon.Attach(f.ctx); err != nil {
		t.Fatal(err)
	}
	s, _ := blockRoot(t, f)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.mon.Verify(ctx) }()
	receiveProbe(t, s.entered)
	cancel()
	if err := receiveProbe(t, done); !errors.Is(err, context.Canceled) || !errors.Is(err, storage.ErrUnreachable) {
		t.Fatalf("cancelled Verify = %v", err)
	}
	if err := f.mon.Ready(); err != nil {
		t.Fatalf("caller cancellation changed shared cache: %v", err)
	}
}

func TestChecksAnnounceExpiryOnceBehindCancelledProbe(t *testing.T) {
	f := newFixture(t)
	if _, err := f.mon.Attach(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.mon.probeTimeout = 50 * time.Millisecond
	s, release := blockRoot(t, f)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.mon.Verify(ctx) }()
	receiveProbe(t, s.entered)
	cancel()
	if err := receiveProbe(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	stale := *f.mon.cache.Load()
	stale.expires = time.Now().Add(-time.Second)
	f.mon.cache.Store(&stale)
	events := f.bus.StorageStatus.Subscribe(t.Context())
	checks := make(chan Status, 8)
	for range 8 {
		go func() { checks <- f.mon.Check(f.ctx) }()
	}
	for range 8 {
		if got := receiveProbe(t, checks); got.State != StateUnreachable {
			t.Fatalf("check behind cancelled I/O = %+v", got)
		}
	}
	if got := s.calls.Load(); got != 1 {
		t.Fatalf("expiry checks started %d I/O workers", got)
	}
	if n := f.eventCount(t, EventUnattached); n != 1 {
		t.Fatalf("concurrent expiry emitted %d rows, want 1", n)
	}
	if ev := receiveProbe(t, events); ev.State != string(StateUnreachable) {
		t.Fatalf("expiry event = %+v", ev)
	}
	select {
	case ev := <-events:
		t.Fatalf("duplicate expiry event: %+v", ev)
	default:
	}
	release()
	waitProbeIdle(t, f.mon)
	if f.mon.Check(f.ctx).State != StateAttached {
		t.Fatal("storage did not recover")
	}
	if ev := receiveProbe(t, events); ev.State != string(StateAttached) {
		t.Fatalf("recovery event = %+v", ev)
	}
}

func TestConcurrentFirstAttachRecordsOneIdentity(t *testing.T) {
	f := newFixture(t)
	// This wrapper counts marker writes without stalling any operation.
	s := &stuckRoot{LocalStorage: f.store}
	f.mon.store = s
	statuses := make(chan Status, 16)
	errs := make(chan error, 16)
	for range 16 {
		go func() {
			status, err := f.mon.Attach(f.ctx)
			statuses <- status
			errs <- err
		}()
	}
	for range 16 {
		if err := receiveProbe(t, errs); err != nil {
			t.Fatal(err)
		}
		if got := receiveProbe(t, statuses); got.State != StateAttached || got.StorageID != f.storedID(t) {
			t.Fatalf("concurrent attach = %+v", got)
		}
	}
	if s.writes.Load() != 1 {
		t.Fatalf("first attach wrote %d markers", s.writes.Load())
	}
	waitProbeIdle(t, f.mon)
}

func TestRunStopsWhileProbeIgnoresCancellation(t *testing.T) {
	f := newFixture(t)
	if _, err := f.mon.Attach(f.ctx); err != nil {
		t.Fatal(err)
	}
	s, _ := blockRoot(t, f)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	done := make(chan struct{})
	go func() { f.mon.Run(ctx); close(done) }()
	receiveProbe(t, s.entered)
	cancel()
	receiveProbe(t, done)
}

func TestTimedOutAdoptDoesNotWriteAfterRootProbeReturns(t *testing.T) {
	f := newFixture(t)
	if _, err := f.mon.Attach(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.mon.probeTimeout = 50 * time.Millisecond
	foreign := mustID(t)
	f.writeMarker(t, foreign)
	s, release := blockRoot(t, f)
	_, err := f.mon.Adopt(f.ctx, "owner")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("adopt = %v", err)
	}
	release()
	waitProbeIdle(t, f.mon)
	if got, err := storage.ReadMarker(f.ctx, f.store); err != nil || got != foreign || s.writes.Load() != 0 {
		t.Fatalf("timed-out adopt changed marker: %q, %v", got, err)
	}
}

type stuckWriteProbe struct {
	*storage.LocalStorage
	release chan struct{}
}

func (s *stuckWriteProbe) ProbeWrite(context.Context) error {
	<-s.release
	return nil
}

func TestLateSuccessfulWriteProbeCannotClearTimeout(t *testing.T) {
	f := newFixture(t)
	if _, err := f.mon.Attach(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.mon.probeTimeout = 50 * time.Millisecond
	s := &stuckWriteProbe{LocalStorage: f.store, release: make(chan struct{})}
	f.mon.store = s
	release := sync.OnceFunc(func() { close(s.release) })
	t.Cleanup(func() { release(); waitProbeIdle(t, f.mon) })
	if err := f.mon.Verify(f.ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Verify = %v", err)
	}
	release()
	waitProbeIdle(t, f.mon)
	if err := f.mon.Ready(); !errors.Is(err, storage.ErrUnreachable) {
		t.Fatalf("late success replaced cached timeout: %v", err)
	}
	if err := f.mon.Verify(f.ctx); err != nil {
		t.Fatalf("fresh successful write probe = %v", err)
	}
}

type delayedMissingMarker struct {
	*storage.LocalStorage
	release chan struct{}
}

func (s *delayedMissingMarker) Open(context.Context, string) (io.ReadSeekCloser, error) {
	<-s.release
	return nil, fs.ErrNotExist
}

func TestCancelledFirstAttachCannotInitializeAfterMarkerReadReturns(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "attach", true: "adopt"}[force], func(t *testing.T) {
			f := newFixture(t)
			f.mon.probeTimeout = 50 * time.Millisecond
			s := &delayedMissingMarker{LocalStorage: f.store, release: make(chan struct{})}
			f.mon.store = s
			release := sync.OnceFunc(func() { close(s.release) })
			t.Cleanup(func() { release(); waitProbeIdle(t, f.mon) })
			var err error
			if force {
				_, err = f.mon.Adopt(f.ctx, "owner")
			} else {
				_, err = f.mon.Attach(f.ctx)
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("first attach = %v", err)
			}
			release()
			waitProbeIdle(t, f.mon)
			if _, err := os.Stat(f.root); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("late attach created a root: %v", err)
			}
			if f.storedID(t) != "" || f.mon.Status().State != StateUnreachable {
				t.Fatal("abandoned attach initialized storage")
			}
		})
	}
}

type blockedEventRepo struct {
	repository.Repository
	entered chan struct{}
	release chan struct{}
}

func (r *blockedEventRepo) CreateEventLog(ctx context.Context, in *repository.EventLogInput) (*repository.EventLog, error) {
	close(r.entered)
	<-r.release
	return r.Repository.CreateEventLog(ctx, in)
}

func TestCachedReadersDoNotWaitForTransitionAudit(t *testing.T) {
	f := newFixture(t)
	if _, err := f.mon.Attach(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.writeMarker(t, mustID(t))
	r := &blockedEventRepo{Repository: f.repo, entered: make(chan struct{}), release: make(chan struct{})}
	f.mon.repo = r
	done := make(chan Status, 1)
	go func() { done <- f.mon.Check(f.ctx) }()
	t.Cleanup(func() { close(r.release); receiveProbe(t, done) })
	receiveProbe(t, r.entered)
	reads := make(chan error, 1)
	go func() { _ = f.mon.Status(); reads <- f.mon.Ready() }()
	if err := receiveProbe(t, reads); !errors.Is(err, storage.ErrUnattached) {
		t.Fatalf("verdict while audit blocked = %v", err)
	}
}

type identityFailureRepo struct {
	repository.Repository
	stage string
	err   error
	fail  atomic.Bool
}

func (r *identityFailureRepo) GetServerSettings(ctx context.Context) (*repository.ServerSettings, error) {
	if r.stage == "load" && r.fail.Load() {
		return nil, r.err
	}
	return r.Repository.GetServerSettings(ctx)
}

func (r *identityFailureRepo) SetStorageID(ctx context.Context, id string) (*repository.ServerSettings, error) {
	if r.stage == "persist" && r.fail.Load() {
		return nil, r.err
	}
	return r.Repository.SetStorageID(ctx, id)
}

func TestIdentityDatabaseFailuresFailClosedAndRecover(t *testing.T) {
	for _, stage := range []string{"load", "persist"} {
		for _, adopt := range []bool{false, true} {
			name := stage + map[bool]string{false: "/attach", true: "/adopt"}[adopt]
			t.Run(name, func(t *testing.T) {
				f := newFixture(t)
				r := &identityFailureRepo{Repository: f.repo, stage: stage, err: errors.New("identity database unavailable")}
				r.fail.Store(true)
				f.mon.repo = r
				var status Status
				var err error
				if adopt {
					status, err = f.mon.Adopt(f.ctx, "owner")
				} else {
					status, err = f.mon.Attach(f.ctx)
				}
				if !errors.Is(err, r.err) || status.Readable() || !errors.Is(f.mon.Ready(), storage.ErrUnreachable) {
					t.Fatalf("database failure = %+v, %v, cached %v", status, err, f.mon.Ready())
				}
				if f.storedID(t) != "" {
					t.Fatal("failed initialization recorded an identity")
				}
				r.fail.Store(false)
				if got := f.mon.Check(f.ctx); got.State != StateAttached || got.StorageID == "" || got.StorageID != f.storedID(t) {
					t.Fatalf("retry after database recovery = %+v", got)
				}
			})
		}
	}
}
