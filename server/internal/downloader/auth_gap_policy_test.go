package downloader

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/hls"
	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/repository"
)

func TestAuthRefreshAppliesPolicyBeforeRestartSplit(t *testing.T) {
	for _, loss := range []string{"expired refetch", "renewal window"} {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/strict=%v", loss, strict), func(t *testing.T) {
				s, d, dir, resolutions, renewedRequests := authGapPolicyFixture(t, loss)
				s.cfg.App.Download.Strict = strict
				s.cfg.App.Download.MaxGapRatio = 0.01
				s.cfg.App.Download.MaxRestartGapSeconds = 1
				result, err := runAuthGapPolicyFixture(t, s, d, dir)
				var gap *hls.GapAbortError
				if !errors.As(err, &gap) || errors.Is(err, hls.ErrPlaylistAuth) || isSplitSignal(err) {
					t.Fatalf("renewal loss bypassed policy: result=%+v error=%v", result, err)
				}
				if d.resume.PendingSplit || d.resume.PendingThresholdSplit || result.EndList {
					t.Fatalf("rejected loss became a successful continuation: resume=%+v result=%+v", d.resume, result)
				}
				if resolutions.Load() != 2 || renewedRequests.Load() != 0 || result.SegmentsDone != 1 {
					t.Fatalf("policy ran after renewed acquisition: resolutions=%d renewed segments=%d result=%+v", resolutions.Load(), renewedRequests.Load(), result)
				}
				job, err := s.repo.GetJob(t.Context(), d.jobID)
				if err != nil {
					t.Fatal(err)
				}
				saved, err := UnmarshalResumeState(job.ResumeState)
				if err != nil || saved.PendingSplit || !saved.ShouldSkip(100) {
					t.Fatalf("failed renewal lost committed checkpoint: saved=%+v error=%v", saved, err)
				}
			})
		}
	}
}

func TestAuthRefreshExpiresRefetchInDurableCheckpoint(t *testing.T) {
	s, d, dir, resolutions, _ := authGapPolicyFixture(t, "expired refetch")
	s.cfg.App.Download.MaxGapRatio = 1
	result, err := runAuthGapPolicyFixture(t, s, d, dir)
	if err != nil || !result.EndList || result.SegmentsDone != 2 || result.SegmentsGaps != 1 || resolutions.Load() != 2 {
		t.Fatalf("permitted expired refetch did not finish: result=%+v resolutions=%d error=%v", result, resolutions.Load(), err)
	}
	job, err := s.repo.GetJob(t.Context(), d.jobID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := UnmarshalResumeState(job.ResumeState)
	if err != nil {
		t.Fatal(err)
	}
	want := []Gap{{MediaSeq: 101, EndMediaSeq: 101, Reason: GapReasonRefetchExpired}}
	if !slices.Equal(saved.Gaps, want) || len(saved.AuthGapSeqs()) != 0 || saved.AccountedFrontierMediaSeq != 102 || saved.PartBytes != 2*int64(len("segment")) {
		t.Fatalf("expired authorization stayed retryable or changed media accounting: %+v", saved)
	}
	if !captureHadWindowRoll(saved) || !saved.HadWindowRoll {
		t.Fatal("expired content did not preserve partial-recording classification")
	}
}

func TestRecoveryWindowSplitPrecedesExpiredRefetchThreshold(t *testing.T) {
	s, d, dir, resolutions, _ := authGapPolicyFixture(t, "recovery window")
	s.cfg.App.Download.MaxGapRatio = 1
	s.cfg.App.Download.MaxRestartGapSeconds = 1
	s.cfg.App.Download.MaxPartBytes = 14
	d.resume.StartPart(100)
	d.resume.NoteCommittedSegment(100, 7, 1)
	d.resume.NoteCommittedSegment(102, 7, 1)
	d.resume.NoteGap(101, GapReasonAuth)
	result, err := runAuthGapPolicyFixture(t, s, d, dir)
	if !errors.Is(err, ErrRestartGapExceeded) || !d.resume.PendingSplit || d.resume.PendingThresholdSplit || resolutions.Load() != 1 {
		t.Fatalf("restored window lost restart-split precedence: result=%+v resume=%+v resolutions=%d error=%v", result, d.resume, resolutions.Load(), err)
	}
	if len(d.resume.AuthGapSeqs()) != 0 || d.resume.PartBytes != 14 {
		t.Fatalf("restart split discarded expired-refetch accounting: %+v", d.resume)
	}
}

func TestRecoveryPolicyDoesNotRefetchSeededCommittedSegments(t *testing.T) {
	s, d, dir, _, repeatedRequests := authGapPolicyFixture(t, "saved above frontier")
	d.resume.StartPart(100)
	d.resume.NoteCommittedSegment(100, 7, 1)
	d.resume.NoteCommittedSegment(102, 7, 1)
	if err := os.WriteFile(filepath.Join(dir, "102.ts"), []byte("segment"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := runAuthGapPolicyFixture(t, s, d, dir)
	if err != nil || !result.EndList || result.SegmentsDone != 3 || result.BytesWritten != 7 || repeatedRequests.Load() != 0 {
		t.Fatalf("saved above-frontier media was acquired or counted again: result=%+v requests=%d error=%v", result, repeatedRequests.Load(), err)
	}
	if d.resume.PartBytes != 21 || d.resume.AccountedFrontierMediaSeq != 102 {
		t.Fatalf("restored media did not join the contiguous frontier: %+v", d.resume)
	}
}

func TestRecoveryAuthBeforeFirstMediaPreservesCursor(t *testing.T) {
	s, d, dir, resolutions, mediaRequests := authGapPolicyFixture(t, "recovery auth")
	d.resume.StartPart(100)
	for seq := int64(100); seq < 110; seq++ {
		d.resume.NoteCommittedSegment(seq, 7, 1)
	}
	result, err := runAuthGapPolicyFixture(t, s, d, dir)
	if err != nil || !result.EndList || result.LastMediaSeq != 110 || result.SegmentsDone != 11 || result.BytesWritten != 7 || resolutions.Load() != 2 || mediaRequests.Load() != 1 {
		t.Fatalf("authorization before the first poll reset the saved cursor: result=%+v resolutions=%d requests=%d error=%v", result, resolutions.Load(), mediaRequests.Load(), err)
	}
	if d.resume.PartBytes != 77 || d.resume.AccountedFrontierMediaSeq != 110 {
		t.Fatalf("renewal changed saved recording metrics: %+v", d.resume)
	}
}

func TestRecoveryDoesNotRefetchResolvedGapsAboveFrontier(t *testing.T) {
	s, d, dir, _, repeatedRequests := authGapPolicyFixture(t, "resolved above frontier")
	s.cfg.App.Download.MaxGapRatio = 0.4
	d.resume.StartPart(100)
	d.resume.NoteCommittedSegment(100, 7, 1)
	d.resume.NoteGap(102, GapReasonFetchFailure)
	d.resume.NoteGap(103, GapReasonStitchedAd)
	d.resume.NoteCommittedSegment(104, 7, 1)
	if err := os.WriteFile(filepath.Join(dir, "104.ts"), []byte("segment"), 0600); err != nil {
		t.Fatal(err)
	}
	wantGaps := slices.Clone(d.resume.Gaps)
	result, err := runAuthGapPolicyFixture(t, s, d, dir)
	if err != nil || !result.EndList || result.SegmentsDone != 3 || result.SegmentsGaps != 1 || result.BytesWritten != 7 || repeatedRequests.Load() != 0 {
		t.Fatalf("saved outcomes were acquired or counted again: result=%+v requests=%d error=%v", result, repeatedRequests.Load(), err)
	}
	if d.resume.PartBytes != 21 || d.resume.AccountedFrontierMediaSeq != 104 || !slices.Equal(d.resume.Gaps, wantGaps) {
		t.Fatalf("restored outcomes changed during acquisition: %+v", d.resume)
	}
}

func authGapPolicyFixture(t *testing.T, loss string) (*Service, *download, string, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var resolutions, firstPolls, renewedRequests atomic.Int32
	var baseURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/gql":
			fmt.Fprint(w, `{"data":{"streamPlaybackAccessToken":{"value":"token","signature":"sig"}}}`)
		case strings.HasPrefix(r.URL.Path, "/api/channel/hls/"):
			attempt := resolutions.Add(1)
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1280x720,CODECS=\"avc1.4D401F,mp4a.40.2\"\n%s/media/%d.m3u8\n", baseURL, attempt)
		case r.URL.Path == "/media/1.m3u8":
			if strings.HasPrefix(loss, "recovery auth") {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if loss == "saved above frontier" {
				fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:100\n#EXTINF:1,\n/segments/1/100.ts\n#EXTINF:1,\n/segments/1/101.ts\n#EXTINF:1,\n/segments/1/102.ts\n#EXT-X-ENDLIST\n")
				return
			}
			if loss == "resolved above frontier" {
				fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:100\n")
				for seq := 100; seq <= 104; seq++ {
					fmt.Fprintf(w, "#EXTINF:1,\n/segments/1/%d.ts\n", seq)
				}
				fmt.Fprint(w, "#EXT-X-ENDLIST\n")
				return
			}
			if loss == "recovery window" {
				fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:105\n#EXTINF:1,\n/segments/1/105.ts\n#EXT-X-ENDLIST\n")
				return
			}
			if loss == "renewal window" && firstPolls.Add(1) > 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:100\n#EXTINF:1,\n/segments/1/100.ts\n")
			if loss == "expired refetch" {
				fmt.Fprint(w, "#EXTINF:1,\n/segments/1/101.ts\n#EXT-X-ENDLIST\n")
			}
		case r.URL.Path == "/segments/1/101.ts":
			if loss == "saved above frontier" || loss == "resolved above frontier" {
				fmt.Fprint(w, "segment")
			} else {
				w.WriteHeader(http.StatusUnauthorized)
			}
		case r.URL.Path == "/media/2.m3u8":
			if loss == "recovery auth window" || loss == "recovery auth expired" {
				head := 110
				if loss == "recovery auth window" {
					head = 115
				}
				fmt.Fprintf(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXTINF:1,\n/segments/2/%d.ts\n#EXT-X-ENDLIST\n", head, head)
				return
			}
			if loss == "recovery auth" {
				fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:100\n")
				for seq := 100; seq <= 110; seq++ {
					fmt.Fprintf(w, "#EXTINF:1,\n/segments/2/%d.ts\n", seq)
				}
				fmt.Fprint(w, "#EXT-X-ENDLIST\n")
				return
			}
			head := 102
			if loss == "renewal window" {
				head = 105
			}
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXTINF:1,\n/segments/2/%d.ts\n#EXT-X-ENDLIST\n", head, head)
		case strings.HasPrefix(r.URL.Path, "/segments/"):
			if strings.HasPrefix(r.URL.Path, "/segments/2/") ||
				loss == "saved above frontier" && strings.HasSuffix(r.URL.Path, "/102.ts") ||
				loss == "resolved above frontier" {
				renewedRequests.Add(1)
			}
			fmt.Fprint(w, "segment")
		default:
			http.NotFound(w, r)
		}
	}))
	baseURL = srv.URL
	t.Cleanup(srv.Close)
	s := newTestService(t, t.TempDir())
	t.Cleanup(s.Shutdown)
	s.cfg.App.Download.SegmentConcurrency = 1
	s.twitch = twitch.New(twitch.Config{HTTPClient: srv.Client(), GQLURL: srv.URL + "/gql", UsherBaseURL: srv.URL}, s.log)
	d := seedWebhookAttempt(t, s, "auth-gap-policy")
	w, err := s.storage.Scratch().New("auth-gap-policy", 1<<16)
	if err != nil {
		t.Fatal(err)
	}
	d.workspace = w
	t.Cleanup(func() { _ = w.Close(true) })
	dir := filepath.Join(w.Dir, "segments")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return s, d, dir, &resolutions, &renewedRequests
}

func runAuthGapPolicyFixture(t *testing.T, s *Service, d *download, dir string) (*hls.JobResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	d.runCtx, d.cancel = ctx, cancel
	emitter := newProgressEmitter(d.jobID, repository.RecordingTypeVideo, make(chan Progress, 32))
	return s.fetchWithAuthRefresh(ctx, context.WithoutCancel(ctx), d, emitter,
		Params{BroadcasterID: "webhook", BroadcasterLogin: "webhook", RecordingType: repository.RecordingTypeVideo}, dir,
		twitch.SelectOptions{Quality: "720", RecordingType: repository.RecordingTypeVideo}, s.log)
}

func TestTerminalGapResolvesAuthWithoutChangingCommittedHistory(t *testing.T) {
	for _, reason := range []GapReason{GapReasonRefetchExpired, GapReasonWindowRolled, GapReasonFetchFailure, GapReasonMalformed, GapReasonStitchedAd} {
		t.Run(string(reason), func(t *testing.T) {
			r := NewResumeState()
			r.StartPart(100)
			r.NoteCommittedSegment(100, 10, 1)
			r.NoteCommittedSegment(102, 20, 2)
			r.NoteGap(101, GapReasonAuth)
			boundary, crossed := r.NoteGapUntilThreshold(101, reason, 30, 0)
			if !crossed || boundary != 102 || r.PartBytes != 30 || r.PartDurationSeconds != 3 || len(r.AuthGapSeqs()) != 0 {
				t.Fatalf("resolved authorization lost threshold or media accounting: boundary=%d crossed=%v resume=%+v", boundary, crossed, r)
			}
			if !slices.Equal(r.Gaps, []Gap{{MediaSeq: 101, EndMediaSeq: 101, Reason: reason}}) {
				t.Fatalf("authorization did not become terminal: %+v", r.Gaps)
			}
			r.NoteGap(100, reason)
			r.NoteGap(101, GapReasonAuth)
			r.NoteGap(101, GapReasonMalformed)
			if len(r.Gaps) != 1 || r.Gaps[0].Reason != reason || r.PartBytes != 30 {
				t.Fatalf("terminal outcome changed committed or permanent history: %+v", r)
			}
		})
	}
}
