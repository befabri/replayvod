package background

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestReservationIncludesSettlementAndConcurrentShutdown(t *testing.T) {
	r := New(map[string]int{"live": 1})
	w, err := r.Reserve("live", "recording")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reserve("live", "another"); !errors.Is(err, ErrCapacity) {
		t.Fatalf("pending claim released capacity: %v", err)
	}
	settling, release := make(chan struct{}), make(chan struct{})
	go w.Run(func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }, func(error) { close(settling); <-release })
	r.Stop()
	<-settling
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := r.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown claimed unsettled work was flushed: %v", err)
	}
	done := make(chan error, 2)
	for range 2 {
		go func() { r.Stop(); done <- r.Wait(t.Context()) }()
	}
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Reserve("live", "after-stop"); !errors.Is(err, ErrStopped) {
		t.Fatalf("admitted after stop: %v", err)
	}
}

func TestScopeJoinsWritersAndContainsChildPanic(t *testing.T) {
	s := NewScope(t.Context())
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	if err := s.Go("writer", false, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := s.Go("broken child", false, func(context.Context) error { panic("broken writer") }); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Join() }()
	<-cancelled
	select {
	case <-done:
		t.Fatal("Join returned while writer was still alive")
	default:
	}
	if err := s.Go("late", false, func(context.Context) error { return nil }); err == nil {
		t.Fatal("child admitted while joining")
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("child panic was lost")
	}
}

func TestQueuedWorkSharesCapacityThroughSettlement(t *testing.T) {
	r := New(map[string]int{"media": 1})
	w, err := r.Reserve("media", "first")
	if err != nil {
		t.Fatal(err)
	}
	settling, release := make(chan struct{}), make(chan struct{})
	go w.Run(func(context.Context) error { return nil }, func(error) { close(settling); <-release })
	<-settling
	w.Release() // Public admission cleanup cannot release an active execution.
	started := make(chan struct{})
	if err := r.StartQueued("media", "second", func(context.Context) error { close(started); return nil }, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
		t.Fatal("queued work bypassed a reservation still settling")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("queued work did not acquire the released slot")
	}
	if err := r.WaitIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.Stop()
}

func TestCancelRetainsOwnershipUntilWriterAndSettlementExit(t *testing.T) {
	r := New(map[string]int{"live": 1})
	w, err := r.Reserve("live", "recording")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reserve("live", "recording"); !errors.Is(err, ErrBusy) {
		t.Fatalf("duplicate key = %v", err)
	}
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	settling, finish := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	cause := errors.New("operator stop")
	go func() {
		done <- w.Run(func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			close(cancelled)
			<-release
			return context.Cause(ctx)
		}, func(error) { close(settling); <-finish })
	}()
	<-started
	if err := w.Run(func(context.Context) error { t.Error("duplicate execution ran"); return nil }, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("second Run = %v", err)
	}
	r.Cancel("unknown", cause)
	if w.Context().Err() != nil {
		t.Fatal("unknown key cancelled another reservation")
	}
	r.Cancel("recording", cause)
	<-cancelled
	for phase := range 2 {
		if r.Used("live") != 1 {
			t.Fatal("cancellation released active ownership")
		}
		if _, err := r.Reserve("live", "next"); !errors.Is(err, ErrCapacity) {
			t.Fatalf("admission during phase %d = %v", phase, err)
		}
		if phase == 0 {
			close(release)
			<-settling
		}
	}
	close(finish)
	if err := <-done; !errors.Is(err, cause) {
		t.Fatalf("cancel cause lost: %v", err)
	}
	if err := r.Start("live", "next", func(context.Context) error { return nil }, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.WaitIdle(t.Context()); err != nil || r.Used("live") != 0 {
		t.Fatalf("settled ownership retained: %v", err)
	}
	r.Stop()
}

func TestQueuedCancellationDoesNotConsumeOrReleaseAnotherPermit(t *testing.T) {
	r := New(map[string]int{"media": 1})
	first, err := r.Reserve("media", "first")
	if err != nil {
		t.Fatal(err)
	}
	settled := make(chan error, 1)
	if err := r.StartQueued("media", "parked", func(context.Context) error {
		t.Error("cancelled queued work ran")
		return nil
	}, func(err error) { settled <- err }); err != nil {
		t.Fatal(err)
	}
	r.Cancel("parked", context.Canceled)
	if err := <-settled; !errors.Is(err, context.Canceled) {
		t.Fatalf("queued cancellation = %v", err)
	}
	if _, err := r.Reserve("media", "third"); !errors.Is(err, ErrCapacity) {
		t.Fatalf("queued cancellation released first permit: %v", err)
	}
	first.Release()
	first.Release()
	if err := r.WaitIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.Stop()
	if err := r.StartQueued("media", "late", func(context.Context) error { return nil }, nil); !errors.Is(err, ErrStopped) {
		t.Fatalf("queued admission after shutdown = %v", err)
	}
}

func TestScopeWaitDrainsWithoutCancellingChildren(t *testing.T) {
	s := NewScope(t.Context())
	started, release := make(chan struct{}), make(chan struct{})
	if err := s.Go("finalizer", false, func(ctx context.Context) error {
		close(started)
		select {
		case <-ctx.Done():
			return errors.New("drain cancelled finalization")
		case <-release:
			return nil
		}
	}); err != nil {
		t.Fatal(err)
	}
	<-started
	done := make(chan error, 1)
	go func() { done <- s.Wait() }()
	select {
	case err := <-done:
		t.Fatalf("Wait returned before finalization: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if err := s.Context().Err(); err != nil {
		t.Fatalf("Wait cancelled scope: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := s.Context().Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait retained its context after all children exited: %v", err)
	}
	if err := s.Go("late", false, func(context.Context) error { return nil }); err == nil {
		t.Fatal("Wait left admission open")
	}
}

func TestFatalChildPanicCancelsSiblingsAndPreservesCause(t *testing.T) {
	s := NewScope(t.Context())
	started, stopped := make(chan struct{}), make(chan error, 1)
	if err := s.Go("sibling", false, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		stopped <- context.Cause(ctx)
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := s.Go("fatal writer", true, func(context.Context) error { panic("failed upload") }); err != nil {
		t.Fatal(err)
	}
	if err := <-stopped; err == nil || !strings.Contains(err.Error(), "fatal writer: worker panic: failed upload") {
		t.Fatalf("fatal cause = %v", err)
	}
	if err := s.Wait(); err == nil || !strings.Contains(err.Error(), "failed upload") {
		t.Fatalf("fatal child outcome = %v", err)
	}
	s = NewScope(t.Context())
	cause := errors.New("recording deferred")
	s.Cancel(cause)
	if !errors.Is(context.Cause(s.Context()), cause) {
		t.Fatal("explicit scope cancellation lost its cause")
	}
	if err := s.Join(); err != nil {
		t.Fatal(err)
	}
}

func TestSettlementPanicReleasesReservationAndPreservesWorkerError(t *testing.T) {
	r := New(map[string]int{"live": 1})
	w, err := r.Reserve("live", "recording")
	if err != nil {
		t.Fatal(err)
	}
	runErr := errors.New("recording failed")
	err = w.Run(func(context.Context) error { return runErr }, func(err error) {
		if !errors.Is(err, runErr) {
			t.Errorf("settlement error = %v", err)
		}
		if _, err := r.Reserve("live", "next"); !errors.Is(err, ErrCapacity) {
			t.Errorf("settlement released capacity: %v", err)
		}
		panic("terminal write failed")
	})
	if !errors.Is(err, runErr) || !strings.Contains(err.Error(), "terminal write failed") {
		t.Fatalf("settlement lost an error: %v", err)
	}
	w, err = r.Reserve("live", "recording")
	if err != nil {
		t.Fatalf("settlement panic retained ownership: %v", err)
	}
	w.Release()
	r.Stop()
	if err := r.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
}
