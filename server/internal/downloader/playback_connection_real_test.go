//go:build ffmpeg

package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/playbackauth"
	"github.com/befabri/replayvod/server/internal/repository"
)

// TestPlaybackConnectionRecoveryPipeline exercises the real recorder, SQLite
// checkpoints, HTTP GQL/Usher/HLS, ffmpeg, ffprobe and local storage. Only
// Twitch is replaced; credentials are synthetic.
func TestPlaybackConnectionRecoveryPipeline(t *testing.T) {
	requireFFmpegHarness(t)
	for _, format := range []string{"ts", "fmp4"} {
		for _, mode := range []string{"startup recovery", "renewal recovery", "revoked", "outage"} {
			t.Run(format+"/"+mode, func(t *testing.T) {
				t.Parallel()
				ctx := context.Background()
				var init []byte
				var segments [][]byte
				if format == "ts" {
					segments = generateTSSegments(t, t.TempDir(), 3)
				} else {
					init, segments = generateFMP4Segments(t, t.TempDir(), 3)
				}
				var h *harnessService
				var base string
				var polls atomic.Int32
				var validations atomic.Int32
				var segmentMu sync.Mutex
				segmentCalls := map[int]int{}
				const token = "synthetic-session-0123456789abcdef"
				edge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/gql" {
						expected := "OAuth " + token
						if mode == "revoked" && validations.Load() > 1 {
							expected = ""
						}
						if r.Header.Get("Authorization") != expected {
							t.Error("wrong playback credential")
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"streamPlaybackAccessToken": map[string]string{"value": "signed-playback", "signature": "signature"}}})
						return
					}
					if r.Header.Get("Authorization") != "" {
						t.Error("session forwarded to CDN")
					}
					if strings.HasPrefix(r.URL.Path, "/api/channel/hls/") {
						fmt.Fprintf(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=600000,RESOLUTION=1280x720,CODECS=\"avc1.4d401f,mp4a.40.2\",FRAME-RATE=10\n%s/media.m3u8\n", base)
						return
					}
					if r.URL.Path == "/media.m3u8" {
						poll := polls.Add(1)
						if mode != "startup recovery" && poll == 2 {
							row, err := h.repo.GetTwitchPlaybackSession(ctx)
							if err != nil {
								t.Error(err)
								w.WriteHeader(500)
								return
							}
							row.CheckedAt = time.Now().Add(-2 * time.Hour).Unix()
							if err := h.repo.UpdateTwitchPlaybackSessionValidation(ctx, row); err != nil {
								t.Error(err)
							}
							w.WriteHeader(http.StatusUnauthorized)
							return
						}
						count := 3
						if mode != "startup recovery" && poll == 1 {
							count = 2
						}
						fmt.Fprint(w, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:100\n")
						if format == "fmp4" {
							fmt.Fprintf(w, "#EXT-X-MAP:URI=\"%s/init.mp4\"\n", base)
						}
						for i := 0; i < count; i++ {
							fmt.Fprintf(w, "#EXTINF:1,\n%s/segment/%d\n", base, i)
						}
						if count == 3 {
							fmt.Fprint(w, "#EXT-X-ENDLIST\n")
						}
						return
					}
					if r.URL.Path == "/init.mp4" {
						_, _ = w.Write(init)
						return
					}
					if strings.HasPrefix(r.URL.Path, "/segment/") {
						i, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/segment/"))
						if err != nil || i < 0 || i >= len(segments) {
							http.NotFound(w, r)
							return
						}
						segmentMu.Lock()
						segmentCalls[i]++
						segmentMu.Unlock()
						_, _ = w.Write(segments[i])
						return
					}
					http.NotFound(w, r)
				}))
				defer edge.Close()
				base = edge.URL
				h = newHarnessService(t, base)
				defer h.svc.Shutdown()
				auth := playbackauth.New(h.repo, strings.Repeat("s", 32), playbackValidatorFunc(func(context.Context, string) (playbackauth.Identity, error) {
					n := validations.Add(1)
					if n > 1 {
						if mode == "revoked" {
							return playbackauth.Identity{}, playbackauth.ErrRejected
						}
						if mode == "outage" || n == 2 {
							return playbackauth.Identity{}, playbackauth.ErrUnavailable
						}
					}
					return playbackauth.Identity{UserID: "123", Login: "viewer"}, nil
				}))
				if _, err := auth.Connect(ctx, token); err != nil {
					t.Fatal(err)
				}
				if mode == "startup recovery" {
					row, _ := h.repo.GetTwitchPlaybackSession(ctx)
					row.CheckedAt = time.Now().Add(-2 * time.Hour).Unix()
					if err := h.repo.UpdateTwitchPlaybackSessionValidation(ctx, row); err != nil {
						t.Fatal(err)
					}
				}
				h.svc.SetPlaybackCredentials(auth)
				if _, err := h.repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "123", BroadcasterLogin: "review", BroadcasterName: "Review"}); err != nil {
					t.Fatal(err)
				}
				jobID, err := h.svc.Start(ctx, Params{BroadcasterID: "123", BroadcasterLogin: "review", Quality: repository.QualityHigh, RecordingType: twitch.RecordingTypeVideo})
				if err != nil {
					t.Fatal(err)
				}
				job, err := h.repo.GetJob(ctx, jobID)
				if err != nil {
					t.Fatal(err)
				}
				want := repository.VideoStatusDone
				wantSegments := 3
				if mode == "outage" {
					want = repository.VideoStatusFailed
					wantSegments = 2
				}
				video := waitForVideoStatus(t, h.repo, job.VideoID, want, 50*time.Second)
				parts, err := h.repo.ListVideoParts(ctx, job.VideoID)
				if err != nil {
					t.Fatal(err)
				}
				if len(parts) != 1 || parts[0].SizeBytes <= 0 || parts[0].DurationSeconds < 1.8 {
					t.Fatalf("captured media was not stored: %+v", parts)
				}
				if _, err := os.Stat(filepath.Join(h.storageDir, "videos", parts[0].Filename)); err != nil {
					t.Fatalf("stored recording: %v", err)
				}
				probed, err := h.svc.probe.Run(ctx, filepath.Join(h.storageDir, "videos", parts[0].Filename))
				if err != nil || probed.VideoStream == nil || probed.Duration < 1.8 {
					t.Fatalf("stored media is not playable: %v", err)
				}
				assertPartRange(t, parts[0], 100, int64(100+wantSegments-1))
				if want == repository.VideoStatusFailed && (video.CompletionKind != repository.CompletionKindPartial || !video.Truncated) {
					t.Fatalf("failure not classified as partial/truncated: %+v", video)
				}
				segmentMu.Lock()
				defer segmentMu.Unlock()
				for i := 0; i < wantSegments; i++ {
					if segmentCalls[i] != 1 {
						t.Errorf("segment %d fetched %d times", i, segmentCalls[i])
					}
				}
				state, err := auth.Status(ctx)
				if err != nil {
					t.Fatal(err)
				}
				wantState := "connected"
				if mode == "revoked" {
					wantState = "reconnect_required"
				}
				if state.State != wantState {
					t.Fatalf("connection state=%s, want %s", state.State, wantState)
				}
			})
		}
	}
}

func TestPlaybackCaptureFailureResumesFinalizationWithoutTwitch(t *testing.T) {
	requireFFmpegHarness(t)
	for _, format := range []string{"ts", "fmp4"} {
		t.Run(format, func(t *testing.T) {
			ctx := context.Background()
			edge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("sealed capture contacted Twitch on restart")
				w.WriteHeader(500)
			}))
			defer edge.Close()
			h := newHarnessService(t, edge.URL)
			defer h.svc.Shutdown()
			if _, err := h.repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "123", BroadcasterLogin: "review", BroadcasterName: "Review"}); err != nil {
				t.Fatal(err)
			}
			const jobID = "interrupted-capture"
			video, err := h.repo.CreateVideo(ctx, &repository.VideoInput{JobID: jobID, Filename: "capture", Status: repository.VideoStatusRunning, Quality: repository.QualityHigh, BroadcasterID: "123", RecordingType: repository.RecordingTypeVideo})
			if err != nil {
				t.Fatal(err)
			}
			state := NewResumeState()
			state.Stage = StagePrepareInput
			state.CaptureError = "Twitch playback could not be renewed; saved the captured portion"
			state.SelectedQuality = "120"
			state.SelectedCodec = repository.CodecH264
			state.SegmentFormat = format
			state.PartStarted = true
			state.PartStartMediaSequence = 100
			state.AccountedFrontierMediaSeq = 99
			segmentDir := filepath.Join(h.scratchDir, jobID, "part01", "segments")
			if err := os.MkdirAll(segmentDir, 0755); err != nil {
				t.Fatal(err)
			}
			var segments [][]byte
			extension := "ts"
			if format == "ts" {
				segments = generateTSSegments(t, t.TempDir(), 2)
			} else {
				init, segs := generateFMP4Segments(t, t.TempDir(), 2)
				segments = segs
				extension = "m4s"
				if err := os.WriteFile(filepath.Join(segmentDir, "init.mp4"), init, 0644); err != nil {
					t.Fatal(err)
				}
			}
			for i, segment := range segments {
				if err := os.WriteFile(filepath.Join(segmentDir, fmt.Sprintf("%d.%s", 100+i, extension)), segment, 0644); err != nil {
					t.Fatal(err)
				}
				state.NoteCommittedSegment(int64(100+i), int64(len(segment)), 1)
			}
			encoded, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h.repo.CreateJob(ctx, &repository.JobInput{ID: jobID, VideoID: video.ID, BroadcasterID: "123", ResumeState: encoded}); err != nil {
				t.Fatal(err)
			}
			if err := h.repo.MarkJobRunning(ctx, jobID); err != nil {
				t.Fatal(err)
			}
			if err := h.svc.Resume(ctx); err != nil {
				t.Fatal(err)
			}
			result := waitForVideoStatus(t, h.repo, video.ID, repository.VideoStatusFailed, 20*time.Second)
			parts, err := h.repo.ListVideoParts(ctx, video.ID)
			if err != nil {
				t.Fatal(err)
			}
			if result.CompletionKind != repository.CompletionKindPartial || len(parts) != 1 || parts[0].SizeBytes <= 0 {
				t.Fatalf("restart did not preserve interrupted footage: completion=%s parts=%+v", result.CompletionKind, parts)
			}
		})
	}
}
