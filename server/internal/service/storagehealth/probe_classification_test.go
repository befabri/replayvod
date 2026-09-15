package storagehealth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/storage"
)

func assertUnreachable(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("probe reported success without reaching a verdict")
	}
	if !errors.Is(err, storage.ErrUnreachable) {
		t.Fatalf("error does not carry ErrUnreachable: %v", err)
	}
	if storage.CanRead(err) {
		t.Fatalf("unfinished probe authorized reads: %v", err)
	}
}

func assertCallerGone(t *testing.T, err error) {
	t.Helper()
	assertUnreachable(t, err)
	if !storage.CallerGone(err) {
		t.Fatalf("caller abandonment not reported as CallerGone: %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error lost the caller's cause: %v", err)
	}
}

func assertProbeDeadline(t *testing.T, err error) {
	t.Helper()
	assertUnreachable(t, err)
	if storage.CallerGone(err) {
		t.Fatalf("stalled backend misreported as caller abandonment: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error lost the deadline cause: %v", err)
	}
}

func attachedFixture(t *testing.T) fixture {
	t.Helper()
	f := newFixture(t)
	if _, err := f.mon.Attach(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.mon.Ready(); err != nil {
		t.Fatalf("fixture did not start healthy: %v", err)
	}
	return f
}

// TestVerify_CallerCancelledBeforeEntryReportsCallerGone covers probe's first
// exit, before the gate.
func TestVerify_CallerCancelledBeforeEntryReportsCallerGone(t *testing.T) {
	f := attachedFixture(t)
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()

	assertCallerGone(t, f.mon.Verify(ctx))

	// One aborted range request must not 503 every other viewer.
	if err := f.mon.Ready(); err != nil {
		t.Fatalf("caller cancellation published a verdict: %v", err)
	}
}

// TestVerify_CallerCancelledWaitingForGateReportsCallerGone covers probe's exit
// from the gate wait.
func TestVerify_CallerCancelledWaitingForGateReportsCallerGone(t *testing.T) {
	f := attachedFixture(t)
	s, release := blockRoot(t, f)

	holder := make(chan error, 1)
	go func() { holder <- f.mon.Verify(f.ctx) }()
	receiveProbe(t, s.entered) // The gate is now held by the stuck probe.

	ctx, cancel := context.WithCancel(f.ctx)
	queued := make(chan error, 1)
	go func() { queued <- f.mon.Verify(ctx) }()
	cancel()

	assertCallerGone(t, receiveProbe(t, queued))
	if err := f.mon.Ready(); err != nil {
		t.Fatalf("a queued caller's cancellation published a verdict: %v", err)
	}
	release()
	receiveProbe(t, holder)
}

// TestVerify_CallerCancelledDuringProbeReportsCallerGone covers probe's exit
// after the backend call.
func TestVerify_CallerCancelledDuringProbeReportsCallerGone(t *testing.T) {
	f := attachedFixture(t)
	s, release := blockRoot(t, f)
	defer release()

	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan error, 1)
	go func() { done <- f.mon.Verify(ctx) }()
	receiveProbe(t, s.entered) // The probe is inside the backend call.
	cancel()

	assertCallerGone(t, receiveProbe(t, done))
	if err := f.mon.Ready(); err != nil {
		t.Fatalf("cancellation mid-probe published a verdict: %v", err)
	}
}

// TestVerify_ProbeDeadlineWithLiveCallerIsNotCallerGone is the mirror: a live
// caller waiting on a stalled backend is a storage outage, never an abandoned
// probe.
func TestVerify_ProbeDeadlineWithLiveCallerIsNotCallerGone(t *testing.T) {
	f := attachedFixture(t)
	f.mon.probeTimeout = 100 * time.Millisecond
	_, release := blockRoot(t, f)
	defer release()

	done := make(chan error, 1)
	go func() { done <- f.mon.Verify(f.ctx) }()

	assertProbeDeadline(t, receiveProbe(t, done))

	// A real stall is evidence about storage and must publish.
	if err := f.mon.Ready(); err == nil {
		t.Fatal("a stalled backend left readiness healthy")
	}
}

// TestVerify_GateWaitDeadlineWithLiveCallerIsNotCallerGone pins the same for a
// caller that reaches its own deadline queued behind a stalled probe while
// staying connected.
func TestVerify_GateWaitDeadlineWithLiveCallerIsNotCallerGone(t *testing.T) {
	f := attachedFixture(t)
	f.mon.probeTimeout = 100 * time.Millisecond
	s, release := blockRoot(t, f)
	defer release()

	holder := make(chan error, 1)
	go func() { holder <- f.mon.Verify(f.ctx) }()
	receiveProbe(t, s.entered)

	queued := make(chan error, 1)
	go func() { queued <- f.mon.Verify(f.ctx) }()

	assertProbeDeadline(t, receiveProbe(t, queued))
	receiveProbe(t, holder)
}
