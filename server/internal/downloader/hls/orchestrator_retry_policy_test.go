package hls

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDrainOutcomes_AuthCannotBypassGapPolicy(t *testing.T) {
	for _, loss := range []string{"worker", "malformed", "window_roll", "missing_callback"} {
		for _, policy := range []struct {
			name  string
			value GapPolicy
			done  int64
		}{
			{"strict", GapPolicy{Strict: true}, 10},
			{"ratio", GapPolicy{MaxGapRatio: 0.01}, 10},
			{"first_content", GapPolicy{MaxGapRatio: 1}, 0},
		} {
			t.Run(loss+"/"+policy.name, func(t *testing.T) {
				result := &JobResult{LastMediaSeq: 99, SegmentsDone: policy.done}
				results := make(chan SegmentResult)
				skips := make(chan SkipEvent)
				var events []SegmentEvent
				var cancelCalls, rangeCalls int
				cfg := &JobConfig{GapPolicy: policy.value}
				if loss != "missing_callback" {
					cfg.OnMidStreamWindowRoll = func(int64, int64) { rangeCalls++ }
				}
				cfg.OnEvent = func(ev SegmentEvent) { events = append(events, ev) }
				cancel := func() { cancelCalls++ }
				// Unbuffered sends make authorization, loss, and the later commit deterministic.
				go func() {
					defer close(results)
					defer close(skips)
					results <- SegmentResult{MediaSeq: 100, Err: &FetchError{Kind: FetchKindAuth, Status: 401}}
					switch loss {
					case "worker":
						results <- SegmentResult{MediaSeq: 101, Err: errors.New("lost response")}
					case "malformed":
						skips <- SkipEvent{MediaSeq: 101, Reason: SkipReasonMalformed}
					default:
						skips <- SkipEvent{MediaSeq: 101, EndMediaSeq: 129, Reason: SkipReasonWindowRolled}
					}
					results <- SegmentResult{MediaSeq: 130, BytesWritten: 2048}
				}()
				abortErr, authErr := drainOutcomes(cfg, result, results, skips, cancel, nil, nil, slog.New(slog.DiscardHandler))
				if abortErr == nil {
					t.Fatalf("authorization bypassed %s policy: auth=%v result=%+v", loss, authErr, result)
				}
				if authErr != nil || errors.Is(abortErr, ErrPlaylistAuth) {
					t.Fatalf("fatal loss remained refreshable: abort=%v auth=%v", abortErr, authErr)
				}
				if loss == "missing_callback" && !strings.Contains(abortErr.Reason, "callback") {
					t.Fatalf("missing range callback: %v", abortErr)
				}
				if rangeCalls != 0 || result.SegmentsGaps != 0 {
					t.Fatalf("rejected loss was accepted: calls=%d result=%+v", rangeCalls, result)
				}
				if cancelCalls != 1 {
					t.Fatalf("cancel calls=%d, want exactly one", cancelCalls)
				}
				if result.LastMediaSeq != 130 || result.SegmentsDone != policy.done+1 || result.BytesWritten != 2048 {
					t.Fatalf("lost committed media after cancellation: %+v", result)
				}
				if len(events) != 2 || events[0].Outcome != OutcomeAuth || events[1].Outcome != OutcomeCommitted {
					t.Fatalf("durable outcomes=%+v, want authorization then commit", events)
				}
			})
		}
	}
}

func TestRun_InheritedGapPolicyCheckedBeforeAcquisition(t *testing.T) {
	for _, tc := range []struct {
		name       string
		policy     GapPolicy
		done, gaps int64
		wantAbort  bool
	}{
		{"ratio_exceeded", GapPolicy{MaxGapRatio: 0.1}, 10, 28, true},
		{"large_inherited_loss", GapPolicy{MaxGapRatio: 0.01}, 100, 9900, true},
		{"strict", GapPolicy{Strict: true}, 100, 1, true},
		{"first_content", GapPolicy{MaxGapRatio: 1}, 0, 1, true},
		{"default_ratio", GapPolicy{}, 10, 1, true},
		{"at_ratio", GapPolicy{MaxGapRatio: 0.1}, 9, 1, false},
		{"first_content_disabled", GapPolicy{MaxGapRatio: 1, SkipFirstContentGuard: true}, 0, 1, false},
		{"fresh_strict", GapPolicy{Strict: true}, 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path == "/playlist.m3u8" {
					_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:200\n#EXTINF:1,\n/seg/200.ts\n#EXT-X-ENDLIST\n"))
					return
				}
				_, _ = w.Write([]byte("segment"))
			}))
			defer srv.Close()
			cfg := newJob(t, srv, t.TempDir())
			cfg.GapPolicy = tc.policy
			cfg.SeedSegmentsDone, cfg.SeedSegmentsGaps = tc.done, tc.gaps
			cfg.StartMediaSeq = 200
			var callbacks []string
			cfg.OnFirstPoll = func(PollResult) { callbacks = append(callbacks, "first") }
			cfg.OnEvent = func(SegmentEvent) { callbacks = append(callbacks, "event") }
			result, err := Run(t.Context(), cfg)
			var abort *GapAbortError
			if tc.wantAbort {
				if !errors.As(err, &abort) {
					t.Fatalf("inherited policy violation: result=%+v err=%v", result, err)
				}
				if requests.Load() != 0 || len(callbacks) != 0 || result.EndList || result.SegmentsDone != tc.done || result.SegmentsGaps != tc.gaps {
					t.Fatalf("invalid history started acquisition: requests=%d callbacks=%v result=%+v", requests.Load(), callbacks, result)
				}
				return
			}
			if err != nil || !result.EndList || result.SegmentsDone != tc.done+1 || result.SegmentsGaps != tc.gaps || !slices.Equal(callbacks, []string{"first", "event"}) {
				t.Fatalf("valid history rejected: callbacks=%v result=%+v err=%v", callbacks, result, err)
			}
		})
	}
}

func TestDrainOutcomes_PostAuthWindowRollRetainsAccounting(t *testing.T) {
	cfg := &JobConfig{GapPolicy: GapPolicy{MaxGapRatio: 1}}
	result := &JobResult{LastMediaSeq: 99, SegmentsDone: 100}
	results := make(chan SegmentResult)
	skips := make(chan SkipEvent)
	var ranges [][2]int64
	cfg.OnMidStreamWindowRoll = func(from, to int64) { ranges = append(ranges, [2]int64{from, to}) }
	var events []SegmentEvent
	cfg.OnEvent = func(ev SegmentEvent) { events = append(events, ev) }
	var cancelCalls int
	go func() {
		defer close(results)
		defer close(skips)
		results <- SegmentResult{MediaSeq: 100, Err: &FetchError{Kind: FetchKindAuth, Status: 401}}
		skips <- SkipEvent{MediaSeq: 101, EndMediaSeq: 129, Reason: SkipReasonWindowRolled}
		results <- SegmentResult{MediaSeq: 130, BytesWritten: 2048}
	}()
	abortErr, authErr := drainOutcomes(cfg, result, results, skips, func() { cancelCalls++ }, nil, nil, slog.New(slog.DiscardHandler))
	if abortErr != nil || !errors.Is(authErr, ErrPlaylistAuth) {
		t.Fatalf("permitted loss stopped renewal: abort=%v auth=%v", abortErr, authErr)
	}
	if !slices.Equal(ranges, [][2]int64{{101, 129}}) || result.SegmentsGaps != 29 || result.SegmentsDone != 101 || result.BytesWritten != 2048 || result.LastMediaSeq != 130 {
		t.Fatalf("lost post-authorization accounting: ranges=%v result=%+v", ranges, result)
	}
	if cancelCalls != 1 || len(events) != 2 || events[0].Outcome != OutcomeAuth || events[1].Outcome != OutcomeCommitted {
		t.Fatalf("post-authorization lifecycle: cancellations=%d events=%+v", cancelCalls, events)
	}
}

func TestDrainOutcomes_PermanentAuthCannotBeRefreshed(t *testing.T) {
	retryable := &FetchError{Kind: FetchKindAuth, Status: 401}
	permanent := &FetchError{Kind: FetchKindAuth, Status: 403, Permanent: true}
	for _, tc := range []struct {
		name        string
		failures    []error
		wantAbort   bool
		wantRefetch []int64
	}{
		{"retryable_then_permanent", []error{retryable, permanent}, false, []int64{100}},
		{"permanent_then_retryable", []error{permanent, retryable}, false, []int64{101}},
		{"policy_then_permanent", []error{errors.New("lost response"), permanent}, true, nil},
		{"permanent_then_policy", []error{permanent, errors.New("lost response")}, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &JobConfig{GapPolicy: GapPolicy{Strict: true}}
			result := &JobResult{SegmentsDone: 10}
			results := make(chan SegmentResult, len(tc.failures)+1)
			for i, err := range tc.failures {
				results <- SegmentResult{MediaSeq: 100 + int64(i), Err: err}
			}
			results <- SegmentResult{MediaSeq: 102, BytesWritten: 512}
			close(results)
			skips := make(chan SkipEvent)
			close(skips)
			var cancelCalls int
			abortErr, authErr := drainOutcomes(cfg, result, results, skips, func() { cancelCalls++ }, nil, nil, slog.New(slog.DiscardHandler))
			if tc.wantAbort {
				if abortErr == nil || authErr != nil {
					t.Fatalf("policy failure was replaced: abort=%v auth=%v", abortErr, authErr)
				}
			} else if abortErr != nil || !errors.Is(authErr, ErrPlaylistAuthPermanent) || errors.Is(authErr, ErrPlaylistAuth) {
				t.Fatalf("permanent restriction remained refreshable: abort=%v auth=%v", abortErr, authErr)
			}
			if cancelCalls != 1 || result.LastMediaSeq != 102 || result.SegmentsDone != 11 || result.BytesWritten != 512 || !slices.Equal(result.AuthErrorSeqs, tc.wantRefetch) {
				t.Fatalf("terminal authorization drain: cancellations=%d result=%+v", cancelCalls, result)
			}
		})
	}
}
