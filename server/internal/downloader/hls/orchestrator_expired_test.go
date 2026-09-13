package hls

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRun_FirstPlaylistLossEnforcesPolicy(t *testing.T) {
	for _, tc := range []struct {
		name        string
		start, head int64
		refetch     []int64
		policy      GapPolicy
	}{
		{"expired_refetch", 100, 100, []int64{10}, GapPolicy{Strict: true}},
		{"expired_zero", 1, 1, []int64{0}, GapPolicy{Strict: true}},
		{"window_strict", 50, 100, nil, GapPolicy{Strict: true}},
		{"window_ratio", 50, 100, nil, GapPolicy{MaxGapRatio: 0.1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mediaRequests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/playlist.m3u8" {
					_, _ = fmt.Fprintf(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXTINF:1,\n/seg/%d.ts\n#EXT-X-ENDLIST\n", tc.head, tc.head)
					return
				}
				mediaRequests.Add(1)
				_, _ = w.Write([]byte("media"))
			}))
			defer srv.Close()
			cfg := newJob(t, srv, t.TempDir())
			cfg.StartMediaSeq, cfg.RefetchSeqs = tc.start, tc.refetch
			cfg.SeedSegmentsDone = 100
			cfg.GapPolicy = tc.policy
			var callbacks []string
			cfg.OnWindowRoll = func(int64, int64, time.Duration) { callbacks = append(callbacks, "window") }
			cfg.OnFirstPoll = func(PollResult) { callbacks = append(callbacks, "first") }
			cfg.OnEvent = func(SegmentEvent) { callbacks = append(callbacks, "event") }
			result, err := Run(t.Context(), cfg)
			var gap *GapAbortError
			if !errors.As(err, &gap) {
				t.Fatalf("first playlist lost media without policy failure: result=%+v err=%v", result, err)
			}
			if result.EndList || result.SegmentsDone != 100 || mediaRequests.Load() != 0 || len(callbacks) != 0 {
				t.Fatalf("policy failure started acquisition or committed loss: requests=%d callbacks=%v result=%+v", mediaRequests.Load(), callbacks, result)
			}
		})
	}
}

func TestRun_FirstPlaylistLossCountsUnion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/playlist.m3u8" {
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:100\n#EXTINF:1,\n/seg/100.ts\n#EXT-X-ENDLIST\n"))
			return
		}
		_, _ = w.Write([]byte("media"))
	}))
	defer srv.Close()
	cfg := newJob(t, srv, t.TempDir())
	cfg.StartMediaSeq = 50
	cfg.RefetchSeqs = []int64{99, 10, 70, 10, 70, 20, 200}
	cfg.SeedSegmentsDone = 100
	cfg.GapPolicy = GapPolicy{MaxGapRatio: 53.0 / 153}
	var ranges [][2]int64
	var expired []int64
	cfg.OnWindowRoll = func(from, to int64, _ time.Duration) { ranges = append(ranges, [2]int64{from, to}) }
	cfg.OnEvent = func(ev SegmentEvent) {
		if ev.Outcome == "refetch_expired" {
			expired = append(expired, ev.MediaSeq)
		}
	}
	result, err := Run(t.Context(), cfg)
	if err != nil || !result.EndList || result.SegmentsDone != 101 || result.SegmentsGaps != 53 {
		t.Fatalf("first playlist union accounting: result=%+v err=%v", result, err)
	}
	if !slices.Equal(ranges, [][2]int64{{50, 99}}) || !slices.Equal(expired, []int64{10, 20, 70, 99, 200}) {
		t.Fatalf("durable loss callbacks: ranges=%v expired=%v", ranges, expired)
	}
}

func TestInitialLossRecoveryOnlyExemptsWindow(t *testing.T) {
	for _, tc := range []struct {
		name       string
		recovering bool
		refetch    []int64
		wantAbort  bool
	}{
		{"default_window_enforces", false, nil, true},
		{"anchored_recovery_window", true, nil, false},
		{"recovery_expired_outside", true, []int64{10}, true},
		{"recovery_expired_inside", true, []int64{70}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			cfg := &JobConfig{Recovering: tc.recovering, GapPolicy: GapPolicy{Strict: true}}
			cfg.OnWindowRoll = func(int64, int64, time.Duration) { calls = append(calls, "window") }
			cfg.OnEvent = func(SegmentEvent) { calls = append(calls, "expired") }
			result := &JobResult{SegmentsDone: 100}
			err := applyInitialLoss(cfg, result, PollResult{WindowRollFrom: 50, WindowRollTo: 99, ExpiredRefetchSeqs: tc.refetch})
			var gap *GapAbortError
			if tc.wantAbort {
				if !errors.As(err, &gap) || len(calls) != 0 || result.SegmentsGaps != 0 {
					t.Fatalf("recovery bypassed refetch loss or changed accounting on rejection: result=%+v calls=%v err=%v", result, calls, err)
				}
			} else if err != nil || !slices.Equal(calls, []string{"window"}) || result.SegmentsGaps != 0 {
				t.Fatalf("anchored recovery lost its split callback: result=%+v calls=%v err=%v", result, calls, err)
			}
		})
	}
}

func TestInitialLossValidatesUnionBeforeRecoveryCallbacks(t *testing.T) {
	var calls []string
	cfg := &JobConfig{Recovering: true, GapPolicy: GapPolicy{MaxGapRatio: 0.015}}
	cfg.OnWindowRoll = func(int64, int64, time.Duration) { calls = append(calls, "window") }
	cfg.OnFirstPoll = func(PollResult) { calls = append(calls, "first") }
	cfg.OnEvent = func(SegmentEvent) { calls = append(calls, "expired") }
	first := PollResult{WindowRollFrom: 50, WindowRollTo: 99, ExpiredRefetchSeqs: []int64{10, 20}}
	result := &JobResult{SegmentsDone: 100}
	var gap *GapAbortError
	if err := applyInitialLoss(cfg, result, first); !errors.As(err, &gap) || len(calls) != 0 {
		t.Fatalf("partial loss callbacks preceded the aggregate policy check: calls=%v err=%v", calls, err)
	}
	cfg.GapPolicy.MaxGapRatio = 1
	if err := applyInitialLoss(cfg, result, first); err != nil || !slices.Equal(calls, []string{"window", "first", "expired", "expired"}) || result.SegmentsGaps != 2 {
		t.Fatalf("recovery callback order changed: result=%+v calls=%v err=%v", result, calls, err)
	}
}

func TestDrainOutcomes_ExpiredRefetchWithinWindowCountsOnce(t *testing.T) {
	for _, refetchFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(refetchFirst), func(t *testing.T) {
			cfg := &JobConfig{GapPolicy: GapPolicy{MaxGapRatio: 19.0 / 119}}
			var expired []int64
			var ranges [][2]int64
			cfg.OnEvent = func(ev SegmentEvent) {
				if ev.Outcome == OutcomeRefetchExpired {
					expired = append(expired, ev.MediaSeq)
				}
			}
			cfg.OnMidStreamWindowRoll = func(from, to int64) { ranges = append(ranges, [2]int64{from, to}) }
			result := &JobResult{SegmentsDone: 100}
			results := make(chan SegmentResult)
			close(results)
			skips := make(chan SkipEvent, 2)
			losses := []SkipEvent{{MediaSeq: 101, EndMediaSeq: 119, Reason: SkipReasonWindowRolled}, {MediaSeq: 110, Reason: SkipReasonRefetchExpired}}
			if refetchFirst {
				slices.Reverse(losses)
			}
			for _, ev := range losses {
				skips <- ev
			}
			close(skips)
			abort, auth := drainOutcomes(cfg, result, results, skips, func() { t.Error("permitted union canceled acquisition") }, nil, nil, slog.New(slog.DiscardHandler))
			if abort != nil || auth != nil || result.SegmentsGaps != 19 || !slices.Equal(expired, []int64{110}) || !slices.Equal(ranges, [][2]int64{{101, 119}}) {
				t.Fatalf("overlapping loss inflated accounting or lost durable transition: result=%+v expired=%v ranges=%v abort=%v auth=%v", result, expired, ranges, abort, auth)
			}
		})
	}
}

func TestRun_SavedSequencesCannotBeRefetchedOrCountedAsLoss(t *testing.T) {
	for _, tc := range []struct {
		name               string
		start, head        int64
		committed, refetch []int64
		policy             GapPolicy
		wantDone, wantGaps int64
		wantRequests       []int64
		wantWindow         bool
	}{
		{"saved_refetch_in_playlist", 100, 100, []int64{100}, []int64{100}, GapPolicy{Strict: true}, 1, 0, nil, false},
		{"saved_refetch_expired", 101, 101, []int64{100}, []int64{100}, GapPolicy{Strict: true}, 2, 0, []int64{101}, false},
		{"saved_only_initial_window", 100, 102, []int64{100, 101}, []int64{100}, GapPolicy{Strict: true}, 3, 0, []int64{102}, false},
		{"saved_inside_initial_loss", 100, 103, []int64{101}, []int64{101}, GapPolicy{MaxGapRatio: 2.0 / 3}, 2, 2, []int64{103}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests []int64
			var mu sync.Mutex
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/playlist.m3u8" {
					_, _ = fmt.Fprintf(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXTINF:1,\n/seg/%d.ts\n#EXT-X-ENDLIST\n", tc.head, tc.head)
					return
				}
				var seq int64
				_, _ = fmt.Sscanf(r.URL.Path, "/seg/%d.ts", &seq)
				mu.Lock()
				requests = append(requests, seq)
				mu.Unlock()
				_, _ = w.Write([]byte("media"))
			}))
			defer srv.Close()
			cfg := newJob(t, srv, t.TempDir())
			cfg.StartMediaSeq, cfg.ResolvedSeqs, cfg.RefetchSeqs = tc.start, tc.committed, tc.refetch
			cfg.SeedSegmentsDone = int64(len(tc.committed))
			cfg.GapPolicy = tc.policy
			var windows int
			if tc.wantWindow {
				cfg.OnWindowRoll = func(int64, int64, time.Duration) { windows++ }
			}
			var events []SegmentEvent
			cfg.OnEvent = func(ev SegmentEvent) { events = append(events, ev) }
			result, err := Run(t.Context(), cfg)
			mu.Lock()
			gotRequests := slices.Clone(requests)
			mu.Unlock()
			if err != nil || !result.EndList || result.SegmentsDone != tc.wantDone || result.SegmentsGaps != tc.wantGaps || result.LastMediaSeq != tc.head || !slices.Equal(gotRequests, tc.wantRequests) {
				t.Fatalf("saved media was fetched, lost, or dropped from cursor: result=%+v requests=%v err=%v", result, gotRequests, err)
			}
			if len(events) != len(tc.wantRequests) || (windows == 1) != tc.wantWindow {
				t.Fatalf("saved media emitted duplicate outcomes: windows=%d events=%+v", windows, events)
			}
		})
	}
}

func TestRun_ResolvedPermanentGapIsNotCountedAgain(t *testing.T) {
	for _, head := range []int64{100, 102} {
		t.Run(fmt.Sprint(head), func(t *testing.T) {
			var mediaRequests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/playlist.m3u8" {
					_, _ = fmt.Fprintf(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXTINF:1,\n/seg/%d.ts\n#EXT-X-ENDLIST\n", head, head)
					return
				}
				mediaRequests.Add(1)
				_, _ = w.Write([]byte("media"))
			}))
			defer srv.Close()
			cfg := newJob(t, srv, t.TempDir())
			cfg.StartMediaSeq = 100
			cfg.ResolvedSeqs = []int64{100}
			cfg.RefetchSeqs = []int64{100}
			cfg.SeedSegmentsDone, cfg.SeedSegmentsGaps = 10, 1
			cfg.GapPolicy.MaxGapRatio = 0.2
			var windows int
			cfg.OnWindowRoll = func(int64, int64, time.Duration) { windows++ }
			var events []SegmentEvent
			cfg.OnEvent = func(ev SegmentEvent) { events = append(events, ev) }
			result, err := Run(t.Context(), cfg)
			if err != nil || !result.EndList || result.LastMediaSeq != head {
				t.Fatalf("resolved history lost its cursor: result=%+v err=%v", result, err)
			}
			if head == 100 {
				if mediaRequests.Load() != 0 || len(events) != 0 || windows != 0 || result.SegmentsDone != 10 || result.SegmentsGaps != 1 {
					t.Fatalf("resolved loss was reacquired: requests=%d events=%v windows=%d result=%+v", mediaRequests.Load(), events, windows, result)
				}
			} else if mediaRequests.Load() != 1 || len(events) != 1 || windows != 1 || result.SegmentsDone != 11 || result.SegmentsGaps != 2 {
				t.Fatalf("window counted earlier permanent loss twice: requests=%d events=%v windows=%d result=%+v", mediaRequests.Load(), events, windows, result)
			}
		})
	}
}

func TestDrainOutcomes_ResolvedWindowNeedsNoGapCallback(t *testing.T) {
	cfg := &JobConfig{GapPolicy: GapPolicy{Strict: true}}
	result := &JobResult{SegmentsDone: 19}
	result.accounted.add(101, 119)
	results := make(chan SegmentResult)
	close(results)
	skips := make(chan SkipEvent, 1)
	skips <- SkipEvent{MediaSeq: 101, EndMediaSeq: 119, Reason: SkipReasonWindowRolled}
	close(skips)
	abort, auth := drainOutcomes(cfg, result, results, skips, func() { t.Error("saved window canceled acquisition") }, nil, nil, slog.New(slog.DiscardHandler))
	if abort != nil || auth != nil || result.SegmentsGaps != 0 || result.SegmentsDone != 19 || result.LastMediaSeq != 119 {
		t.Fatalf("saved window required loss callback: result=%+v abort=%v auth=%v", result, abort, auth)
	}
}
