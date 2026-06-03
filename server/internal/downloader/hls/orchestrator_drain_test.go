package hls

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync/atomic"
	"testing"
)

func TestDrainOutcomes_MidStreamWindowRollIsNonAbortingRangeGap(t *testing.T) {
	cfg := &JobConfig{GapPolicy: GapPolicy{MaxGapRatio: 1}}
	result := &JobResult{LastMediaSeq: 101, SegmentsDone: 100}
	results := make(chan SegmentResult)
	skipEvents := make(chan SkipEvent, 1)

	var calls int
	var gotFrom, gotTo int64
	cfg.OnMidStreamWindowRoll = func(from, to int64) {
		calls++
		gotFrom, gotTo = from, to
	}

	close(results)
	skipEvents <- SkipEvent{MediaSeq: 102, EndMediaSeq: 129, Reason: SkipReasonWindowRolled}
	close(skipEvents)

	abortErr, authErr := drainOutcomes(cfg, result, results, skipEvents, func() {}, nil, slog.New(slog.DiscardHandler))

	if abortErr != nil || authErr != nil {
		t.Fatalf("window roll must not abort: abortErr=%v authErr=%v", abortErr, authErr)
	}
	if calls != 1 || gotFrom != 102 || gotTo != 129 {
		t.Fatalf("OnMidStreamWindowRoll calls=%d range=[%d,%d], want 1 call over [102,129]", calls, gotFrom, gotTo)
	}
	if result.LastMediaSeq != 129 {
		t.Fatalf("LastMediaSeq = %d, want 129 (advanced to range end)", result.LastMediaSeq)
	}
	if result.SegmentsGaps != 28 {
		t.Fatalf("SegmentsGaps = %d, want 28 (accepted window-roll range)", result.SegmentsGaps)
	}
}

func TestDrainOutcomes_MidStreamWindowRollWithoutCallbackDoesNotAdvanceFrontier(t *testing.T) {
	cfg := &JobConfig{GapPolicy: GapPolicy{MaxGapRatio: 1}}
	result := &JobResult{LastMediaSeq: 101, SegmentsDone: 100}
	results := make(chan SegmentResult)
	skipEvents := make(chan SkipEvent, 1)
	close(results)
	skipEvents <- SkipEvent{MediaSeq: 102, EndMediaSeq: 129, Reason: SkipReasonWindowRolled}
	close(skipEvents)

	var cancelCalled atomic.Int32
	abortErr, authErr := drainOutcomes(cfg, result, results, skipEvents, func() { cancelCalled.Add(1) }, nil, slog.New(slog.DiscardHandler))
	if authErr != nil {
		t.Fatalf("authErr=%v, want nil", authErr)
	}
	if abortErr == nil {
		t.Fatal("abortErr is nil; want missing window-roll callback to fail the job")
	}
	if result.LastMediaSeq != 101 {
		t.Fatalf("LastMediaSeq = %d, want 101; missing callback must not advance durable frontier", result.LastMediaSeq)
	}
	if result.SegmentsGaps != 0 {
		t.Fatalf("SegmentsGaps = %d, want 0; unrecorded window roll must not be accepted", result.SegmentsGaps)
	}
	if cancelCalled.Load() != 1 {
		t.Fatalf("cancel called %d times, want 1", cancelCalled.Load())
	}
}

func TestDrainOutcomes_MidStreamWindowRollTripsGapRatio(t *testing.T) {
	cfg := &JobConfig{GapPolicy: GapPolicy{MaxGapRatio: 0.10}}
	result := &JobResult{LastMediaSeq: 101, SegmentsDone: 10}
	results := make(chan SegmentResult)
	skipEvents := make(chan SkipEvent, 1)
	close(results)
	skipEvents <- SkipEvent{MediaSeq: 102, EndMediaSeq: 129, Reason: SkipReasonWindowRolled}
	close(skipEvents)

	var calls int
	cfg.OnMidStreamWindowRoll = func(from, to int64) { calls++ }
	var cancelCalled atomic.Int32
	abortErr, authErr := drainOutcomes(cfg, result, results, skipEvents, func() { cancelCalled.Add(1) }, nil, slog.New(slog.DiscardHandler))
	if authErr != nil {
		t.Fatalf("authErr=%v, want nil", authErr)
	}
	if abortErr == nil {
		t.Fatal("abortErr is nil; want aggregate gap-ratio abort")
	}
	if abortErr.LastSeq != 129 {
		t.Fatalf("abortErr.LastSeq=%d, want 129", abortErr.LastSeq)
	}
	if calls != 0 {
		t.Fatalf("OnMidStreamWindowRoll calls=%d, want 0 for policy-aborted roll", calls)
	}
	if result.LastMediaSeq != 129 {
		t.Fatalf("LastMediaSeq = %d, want 129; callback existed so the skip frontier is known", result.LastMediaSeq)
	}
	if result.SegmentsGaps != 0 {
		t.Fatalf("SegmentsGaps = %d, want 0; trigger gap aborts instead of being accepted", result.SegmentsGaps)
	}
	if cancelCalled.Load() != 1 {
		t.Fatalf("cancel called %d times, want 1", cancelCalled.Load())
	}
}

// TestDrainOutcomes_PostAuthSuccessStillCounted is the regression
// guard for the fix to the second-pass review finding:
// "OnEvent is still not fully exact after an auth/abort boundary."
// Previously the drain loop advanced LastMediaSeq for post-abort
// outcomes but short-circuited before the counter + OnEvent path,
// so a seg that landed on disk after the auth trigger would be
// invisible to resume state even though the next attempt would
// skip past it.
//
// The drain helper is fed by hand so the race window (worker
// completes a commit after cancel() fires but before its SegmentResult
// is drained) is deterministic; wiring this through real HTTP +
// the pool is hostile to the test runner because cancellation
// propagates to in-flight fetches and kills them before they can
// succeed.
func TestDrainOutcomes_PostAuthSuccessStillCounted(t *testing.T) {
	cfg := &JobConfig{}
	result := &JobResult{}
	results := make(chan SegmentResult, 4)
	skipEvents := make(chan SkipEvent, 4)

	var cancelCalled atomic.Int32
	authSeen := make(chan struct{})
	cancel := func() {
		cancelCalled.Add(1)
		close(authSeen)
	}

	var events []SegmentEvent
	cfg.OnEvent = func(ev SegmentEvent) { events = append(events, ev) }

	// Seq 10: auth failure — latches authErr + triggers cancel.
	// Seq 11: committed success delivered AFTER the auth marker
	//   is latched (what a worker finishing its in-flight fetch
	//   would look like once the drain had already processed the
	//   auth).
	// Seq 12: stitched-ad skip arriving after abort.
	// Seq 13: non-auth fetch error arriving after abort — counted
	//   as an accepted gap since the file is permanently lost.
	//
	// Drive the drain concurrently: push the auth first, wait for
	// cancel() to confirm the latch, then push the post-abort
	// events. Go's select picks pseudo-randomly among ready cases,
	// so pre-filling both channels would make the "auth is
	// processed first" ordering non-deterministic.
	done := make(chan struct {
		abortErr *GapAbortError
		authErr  error
	})
	go func() {
		abortErr, authErr := drainOutcomes(cfg, result, results, skipEvents, cancel, nil, slog.New(slog.DiscardHandler))
		done <- struct {
			abortErr *GapAbortError
			authErr  error
		}{abortErr, authErr}
	}()

	results <- SegmentResult{MediaSeq: 10, Err: &FetchError{Kind: FetchKindAuth, Status: 401}}
	<-authSeen
	results <- SegmentResult{MediaSeq: 11, BytesWritten: 2048}
	skipEvents <- SkipEvent{MediaSeq: 12, Reason: SkipReasonStitchedAd}
	results <- SegmentResult{MediaSeq: 13, Err: errors.New("transport blew up")}
	close(results)
	close(skipEvents)

	out := <-done
	abortErr, authErr := out.abortErr, out.authErr

	if authErr == nil {
		t.Fatal("authErr is nil; want auth escalation")
	}
	if !errors.Is(authErr, ErrPlaylistAuth) {
		t.Errorf("authErr %v does not wrap ErrPlaylistAuth", authErr)
	}
	if abortErr != nil {
		t.Errorf("abortErr=%v; want nil (auth path, not gap-ratio path)", abortErr)
	}

	// cancel() called exactly once — the first auth latch only.
	// Subsequent post-abort outcomes must not re-trigger.
	if got := cancelCalled.Load(); got != 1 {
		t.Errorf("cancel called %d times, want 1", got)
	}

	if result.LastMediaSeq != 13 {
		t.Errorf("LastMediaSeq=%d, want 13", result.LastMediaSeq)
	}
	if result.SegmentsDone != 1 {
		t.Errorf("SegmentsDone=%d, want 1 (post-auth commit counts)", result.SegmentsDone)
	}
	if result.BytesWritten != 2048 {
		t.Errorf("BytesWritten=%d, want 2048", result.BytesWritten)
	}
	if result.SegmentsAdGaps != 1 {
		t.Errorf("SegmentsAdGaps=%d, want 1 (post-auth ad skip counts)", result.SegmentsAdGaps)
	}
	if result.SegmentsGaps != 1 {
		t.Errorf("SegmentsGaps=%d, want 1 (post-auth fetch error accepted as gap)", result.SegmentsGaps)
	}

	// OnEvent must see every drained outcome exactly once. Auth
	// happens-before the post-abort pushes (the test waits on
	// authSeen), so seq 10 is index 0 by construction. The post-
	// abort events can interleave — select picks pseudo-randomly
	// among ready cases — so assert that set without pinning order.
	if len(events) != 4 {
		t.Fatalf("len(events)=%d, want 4; events=%+v", len(events), events)
	}
	if events[0].MediaSeq != 10 || events[0].Outcome != OutcomeAuth {
		t.Errorf("events[0]=%+v, want auth for seq 10", events[0])
	}
	if events[0].Err == nil {
		t.Error("events[0] (auth) has nil Err")
	}
	post := map[int64]SegmentEvent{}
	for _, ev := range events[1:] {
		post[ev.MediaSeq] = ev
	}
	if ev, ok := post[11]; !ok {
		t.Error("missing committed event for seq 11 (post-auth success)")
	} else if ev.Outcome != OutcomeCommitted || ev.BytesWritten != 2048 {
		t.Errorf("seq 11 event=%+v, want committed w/ 2048 bytes", ev)
	}
	if ev, ok := post[12]; !ok {
		t.Error("missing ad_skipped event for seq 12")
	} else if ev.Outcome != OutcomeAdSkipped {
		t.Errorf("seq 12 event=%+v, want ad_skipped", ev)
	}
	if ev, ok := post[13]; !ok {
		t.Error("missing gap_accepted event for seq 13")
	} else if ev.Outcome != OutcomeGapAccepted || ev.Err == nil {
		t.Errorf("seq 13 event=%+v, want gap_accepted with non-nil Err", ev)
	}
}

func TestDrainOutcomes_ContextCanceledFetchDoesNotResolveSegment(t *testing.T) {
	cfg := &JobConfig{}
	result := &JobResult{LastMediaSeq: 99}
	results := make(chan SegmentResult, 1)
	skipEvents := make(chan SkipEvent)
	close(skipEvents)

	var events []SegmentEvent
	cfg.OnEvent = func(ev SegmentEvent) { events = append(events, ev) }
	var cancelCalled atomic.Int32
	cancel := func() { cancelCalled.Add(1) }

	results <- SegmentResult{MediaSeq: 100, Err: context.Canceled}
	close(results)

	abortErr, authErr := drainOutcomes(cfg, result, results, skipEvents, cancel, func() bool { return true }, slog.New(slog.DiscardHandler))
	if authErr != nil {
		t.Fatalf("authErr=%v, want nil for parent/scoped context cancellation", authErr)
	}
	if abortErr != nil {
		t.Fatalf("abortErr=%v, want nil; canceled fetches must not trip gap policy", abortErr)
	}
	if result.LastMediaSeq != 99 {
		t.Fatalf("LastMediaSeq=%d, want 99; canceled seq 100 is not durable/resolved work", result.LastMediaSeq)
	}
	if result.SegmentsDone != 0 || result.SegmentsGaps != 0 || result.BytesWritten != 0 {
		t.Fatalf("canceled fetch changed counters: done=%d gaps=%d bytes=%d",
			result.SegmentsDone, result.SegmentsGaps, result.BytesWritten)
	}
	if result.SegmentsCanceled != 1 {
		t.Fatalf("SegmentsCanceled=%d, want 1", result.SegmentsCanceled)
	}
	if !slices.Equal(result.CanceledSeqs, []int64{100}) {
		t.Fatalf("CanceledSeqs=%v, want [100]", result.CanceledSeqs)
	}
	if len(events) != 0 {
		t.Fatalf("events=%+v, want none for canceled unresolved segment", events)
	}
	if cancelCalled.Load() != 0 {
		t.Fatalf("cancel called %d times, want 0", cancelCalled.Load())
	}
}

func TestDrainOutcomes_ContextCanceledAfterPlaylistGoneIsGap(t *testing.T) {
	cfg := &JobConfig{GapPolicy: GapPolicy{MaxGapRatio: 1}}
	result := &JobResult{LastMediaSeq: 100, SegmentsDone: 10}
	results := make(chan SegmentResult, 1)
	skipEvents := make(chan SkipEvent)
	close(skipEvents)

	var events []SegmentEvent
	cfg.OnEvent = func(ev SegmentEvent) { events = append(events, ev) }
	var cancelCalled atomic.Int32
	cancel := func() { cancelCalled.Add(1) }

	results <- SegmentResult{MediaSeq: 101, Err: context.Canceled}
	close(results)

	abortErr, authErr := drainOutcomes(cfg, result, results, skipEvents, cancel, func() bool { return false }, slog.New(slog.DiscardHandler))
	if authErr != nil {
		t.Fatalf("authErr=%v, want nil", authErr)
	}
	if abortErr != nil {
		t.Fatalf("abortErr=%v, want nil; canceled old-variant segment should be accounted as tolerated gap", abortErr)
	}
	if result.LastMediaSeq != 101 {
		t.Fatalf("LastMediaSeq=%d, want 101", result.LastMediaSeq)
	}
	if result.SegmentsGaps != 1 {
		t.Fatalf("SegmentsGaps=%d, want 1", result.SegmentsGaps)
	}
	if result.SegmentsCanceled != 0 {
		t.Fatalf("SegmentsCanceled=%d, want 0; playlist-gone canceled segment is accounted as old-variant gap", result.SegmentsCanceled)
	}
	if len(events) != 1 || events[0].MediaSeq != 101 || events[0].Outcome != OutcomeGapAccepted {
		t.Fatalf("events=%+v, want one gap_accepted event for seq 101", events)
	}
	if cancelCalled.Load() != 0 {
		t.Fatalf("cancel called %d times, want 0", cancelCalled.Load())
	}
}
