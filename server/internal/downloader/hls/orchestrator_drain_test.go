package hls

import (
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
)

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
		abortErr, authErr := drainOutcomes(cfg, result, results, skipEvents, cancel, slog.New(slog.DiscardHandler))
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
