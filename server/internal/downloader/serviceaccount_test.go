package downloader

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func discardLog() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestServiceAccount_ReturnsEmptyWhenUnconfigured(t *testing.T) {
	sa := newServiceAccount("", discardLog())
	if tok := sa.Token(context.Background()); tok != "" {
		t.Errorf("Token()=%q, want empty for unconfigured service account", tok)
	}
}

func TestServiceAccount_ReturnsEmptyWhenNoRefresher(t *testing.T) {
	// Configured but no refresher wired — caller should get ""
	// and log (tested via warn path; we just check the return).
	sa := newServiceAccount("refresh-tok", discardLog())
	if tok := sa.Token(context.Background()); tok != "" {
		t.Errorf("Token()=%q, want empty when no refresher", tok)
	}
}

func TestServiceAccount_CallsRefresherOnce(t *testing.T) {
	// First call refreshes; second call returns cached value
	// without hitting the refresher again.
	var calls int32
	refresher := func(_ context.Context, rt string) (string, time.Time, error) {
		atomic.AddInt32(&calls, 1)
		if rt != "refresh-tok" {
			t.Errorf("refresher got refreshToken=%q, want refresh-tok", rt)
		}
		return "access-xyz", time.Now().Add(4 * time.Hour), nil
	}
	sa := newServiceAccount("refresh-tok", discardLog())
	sa.setRefresher(refresher)

	if got := sa.Token(context.Background()); got != "access-xyz" {
		t.Errorf("first Token()=%q, want access-xyz", got)
	}
	if got := sa.Token(context.Background()); got != "access-xyz" {
		t.Errorf("cached Token()=%q, want access-xyz", got)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("refresher calls=%d, want 1 (cache should have served second call)", n)
	}
}

func TestServiceAccount_RefreshesWhenExpired(t *testing.T) {
	// First call: token expires in 30s (under the 60s slack),
	// so Token() still treats it as expired and refreshes.
	var calls int32
	refresher := func(_ context.Context, _ string) (string, time.Time, error) {
		n := atomic.AddInt32(&calls, 1)
		expiresAt := time.Now().Add(30 * time.Second) // within 60s slack → expired
		if n >= 2 {
			expiresAt = time.Now().Add(time.Hour)
		}
		return "access-" + string(rune('a'+n-1)), expiresAt, nil
	}
	sa := newServiceAccount("refresh-tok", discardLog())
	sa.setRefresher(refresher)

	first := sa.Token(context.Background())
	second := sa.Token(context.Background())
	if first != "access-a" || second != "access-b" {
		t.Errorf("tokens=%q,%q, want access-a,access-b", first, second)
	}
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Errorf("refresher calls=%d, want 2", n)
	}
}

func TestServiceAccount_RefreshFailureFallsBackToAnonymous(t *testing.T) {
	// A refresh-token exchange that errors must NOT fail the
	// caller. Token() returns "" and the pipeline proceeds as
	// anonymous.
	var calls int32
	refresher := func(_ context.Context, _ string) (string, time.Time, error) {
		atomic.AddInt32(&calls, 1)
		return "", time.Time{}, errors.New("oauth endpoint down")
	}
	sa := newServiceAccount("refresh-tok", discardLog())
	sa.setRefresher(refresher)

	if got := sa.Token(context.Background()); got != "" {
		t.Errorf("Token()=%q, want empty on refresh failure", got)
	}
	// A second call should retry (the first attempt didn't
	// cache anything); no wedging on a single bad refresh.
	if got := sa.Token(context.Background()); got != "" {
		t.Errorf("second Token()=%q, want empty", got)
	}
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Errorf("refresher calls=%d, want 2 (no cache on failure)", n)
	}
}

func TestServiceAccount_SingleFlightOnConcurrentCallers(t *testing.T) {
	// 10 goroutines concurrently calling Token() on a cold cache
	// must result in exactly one refresher invocation — all
	// waiters share the same result.
	var calls int32
	gate := make(chan struct{})
	refresher := func(_ context.Context, _ string) (string, time.Time, error) {
		atomic.AddInt32(&calls, 1)
		<-gate // hold the refresh until every goroutine is blocked on it
		return "access-single-flight", time.Now().Add(time.Hour), nil
	}
	sa := newServiceAccount("refresh-tok", discardLog())
	sa.setRefresher(refresher)

	var wg sync.WaitGroup
	results := make(chan string, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- sa.Token(context.Background())
		}()
	}
	// Give the waiters time to pile up on the inflight channel.
	time.Sleep(20 * time.Millisecond)
	close(gate)
	wg.Wait()
	close(results)

	seen := 0
	for r := range results {
		if r != "access-single-flight" {
			t.Errorf("got token %q, want access-single-flight", r)
		}
		seen++
	}
	if seen != 10 {
		t.Errorf("got %d results, want 10", seen)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("refresher calls=%d, want 1 (single-flight)", n)
	}
}

func TestServiceAccount_CtxCancelDuringRefresh(t *testing.T) {
	// Token() must honor ctx cancellation even if a refresh is
	// in flight. The caller cancels while waiting; Token
	// returns "" rather than blocking forever.
	block := make(chan struct{})
	refresher := func(ctx context.Context, _ string) (string, time.Time, error) {
		select {
		case <-block:
		case <-ctx.Done():
			return "", time.Time{}, ctx.Err()
		}
		return "late", time.Now().Add(time.Hour), nil
	}
	sa := newServiceAccount("refresh-tok", discardLog())
	sa.setRefresher(refresher)

	// Kick off one goroutine that gets the inflight slot and
	// blocks the refresher.
	first := make(chan string)
	go func() { first <- sa.Token(context.Background()) }()
	time.Sleep(10 * time.Millisecond)

	// Second caller comes in with a canceled ctx.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := sa.Token(ctx)
	if got != "" {
		t.Errorf("canceled caller Token()=%q, want empty", got)
	}
	// Unblock the first caller so the goroutine exits cleanly.
	close(block)
	<-first
}
