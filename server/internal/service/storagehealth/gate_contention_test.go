package storagehealth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/storage"
)

// pacedRoot holds each root probe inside the backend until the test releases
// it, so one probe can be held while another queues behind it. Like a wedged
// mount, it ignores cancellation.
type pacedRoot struct {
	*storage.LocalStorage
	entered chan chan struct{}
	calls   atomic.Int32
}

func (p *pacedRoot) ProbeRoot(context.Context) error {
	p.calls.Add(1)
	release := make(chan struct{})
	p.entered <- release
	<-release
	return nil
}

func (p *pacedRoot) ProbeWrite(context.Context) error { return nil }

// takeProbe blocks until a probe reaches the backend and returns its release,
// so gate ownership is observed rather than guessed from timing.
func takeProbe(t *testing.T, p *pacedRoot) func() {
	t.Helper()
	select {
	case release := <-p.entered:
		done := sync.OnceFunc(func() { close(release) })
		t.Cleanup(done)
		return done
	case <-time.After(10 * time.Second):
		t.Fatal("probe never reached the backend")
		return func() {}
	}
}

func awaitErr(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("probe never returned")
		return nil
	}
}

// TestQueuedProbeDeadlineDoesNotPublishAFalseOutage pins the contended case: a
// probe that spends its budget queued and then times out inside a backend that
// never failed must fail its own caller without publishing an outage to every
// other reader.
func TestQueuedProbeDeadlineDoesNotPublishAFalseOutage(t *testing.T) {
	f := newFixture(t)
	if _, err := f.mon.Attach(f.ctx); err != nil {
		t.Fatal(err)
	}
	paced := &pacedRoot{LocalStorage: f.store, entered: make(chan chan struct{}, 8)}
	f.mon.store = paced
	f.mon.probeTimeout = 2 * time.Second
	f.mon.interval = time.Minute // Far longer than the run, so expiry cannot be the cause.

	first := make(chan error, 1)
	go func() { first <- f.mon.Verify(f.ctx) }()
	releaseFirst := takeProbe(t, paced)

	// The second probe queues behind the first, burning half its 2s budget.
	second := make(chan error, 1)
	go func() { second <- f.mon.Verify(f.ctx) }()
	time.Sleep(time.Second)

	releaseFirst()
	if err := awaitErr(t, first); err != nil {
		t.Fatalf("healthy backend reported %v", err)
	}
	if err := f.mon.Ready(); err != nil {
		t.Fatalf("healthy probe did not publish a readable verdict: %v", err)
	}

	// Waiting for the second probe to reach the backend makes its deadline
	// attributable to the backend rather than to the queue.
	releaseSecond := takeProbe(t, paced)
	err := awaitErr(t, second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued probe = %v, want a deadline", err)
	}

	if err := f.mon.Ready(); err != nil {
		t.Fatalf("a queued probe's deadline published a false outage: %v", err)
	}
	if got := f.mon.Status().State; got != StateAttached {
		t.Fatalf("state = %s, want attached; the backend never failed a probe", got)
	}
	if got := paced.calls.Load(); got != 2 {
		t.Fatalf("started %d probes, want 2", got)
	}
	releaseSecond()
	waitProbeIdle(t, f.mon)
}

// TestBrieflyQueuedProbeDeadlineStillPublishesTheOutage pins the handoff case:
// a short wait for the gate is not a queue, the backend still had nearly the
// whole budget, so a deadline after it is the backend's own failure and
// publishes. Without this the rule above could excuse a stall behind any probe
// that happened to finish a moment earlier.
func TestBrieflyQueuedProbeDeadlineStillPublishesTheOutage(t *testing.T) {
	f := newFixture(t)
	if _, err := f.mon.Attach(f.ctx); err != nil {
		t.Fatal(err)
	}
	waitProbeIdle(t, f.mon)
	paced := &pacedRoot{LocalStorage: f.store, entered: make(chan chan struct{}, 8)}
	f.mon.store = paced
	f.mon.probeTimeout = 2 * time.Second
	f.mon.interval = time.Minute

	first := make(chan error, 1)
	go func() { first <- f.mon.Verify(f.ctx) }()
	releaseFirst := takeProbe(t, paced)

	// The second probe waits only for the first to hand the gate over, a small
	// fraction of the budget however far the scheduler stretches it.
	second := make(chan error, 1)
	go func() { second <- f.mon.Verify(f.ctx) }()
	time.Sleep(10 * time.Millisecond)
	releaseFirst()
	if err := awaitErr(t, first); err != nil {
		t.Fatalf("healthy backend reported %v", err)
	}

	releaseSecond := takeProbe(t, paced)
	if err := awaitErr(t, second); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stalled probe = %v, want a deadline", err)
	}
	if err := f.mon.Ready(); !errors.Is(err, storage.ErrUnreachable) {
		t.Fatalf("a stall behind a brief handoff was never published: %v", err)
	}
	if got := f.mon.Status().State; got != StateUnreachable {
		t.Fatalf("state = %s, want unreachable", got)
	}
	releaseSecond()
	waitProbeIdle(t, f.mon)
}

// TestUnqueuedProbeDeadlinePublishesTheOutagePromptly is the mirror: a probe
// that goes straight through gives the backend the whole budget, so its
// deadline is the backend's own failure and publishes at once, without waiting
// for the cached verdict to expire.
func TestUnqueuedProbeDeadlinePublishesTheOutagePromptly(t *testing.T) {
	f := newFixture(t)
	if _, err := f.mon.Attach(f.ctx); err != nil {
		t.Fatal(err)
	}
	waitProbeIdle(t, f.mon)
	paced := &pacedRoot{LocalStorage: f.store, entered: make(chan chan struct{}, 8)}
	f.mon.store = paced
	f.mon.probeTimeout = 200 * time.Millisecond
	f.mon.interval = time.Minute // Far longer than the run, so expiry cannot be the cause.

	done := make(chan error, 1)
	go func() { done <- f.mon.Verify(f.ctx) }()
	release := takeProbe(t, paced)

	if err := awaitErr(t, done); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stalled probe = %v, want a deadline", err)
	}
	if err := f.mon.Ready(); !errors.Is(err, storage.ErrUnreachable) {
		t.Fatalf("a stalled backend was never published: %v", err)
	}
	if got := f.mon.Status().State; got != StateUnreachable {
		t.Fatalf("state = %s, want unreachable", got)
	}
	release()
	waitProbeIdle(t, f.mon)
}
