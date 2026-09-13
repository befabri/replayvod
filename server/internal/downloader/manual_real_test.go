//go:build ffmpeg

package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/ptr"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	provider "github.com/befabri/replayvod/server/internal/twitch"
)

type heldFinalizerStorage struct {
	*storage.LocalStorage
	entered, release chan struct{}
	first            atomic.Bool
}

func (b *heldFinalizerStorage) Save(ctx context.Context, key string, body io.Reader) error {
	if strings.HasSuffix(key, ".mp4") && b.first.CompareAndSwap(false, true) {
		close(b.entered)
		select {
		case <-b.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return b.LocalStorage.Save(ctx, key, body)
}

// Runs the complete HLS -> ffmpeg -> managed publication -> terminal pipeline.
// The first finalizer is held while another broadcast uses the intent's slot.
func TestManualRestartWindowsOverlapFinalization(t *testing.T) {
	requireFFmpegHarness(t)
	edge := newTwitchEdge(t, twitchEdgeOpts{tsCount: 3, windowA: 3, baseSeqA: 100, aEndlist: 3})
	raw, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	held := &heldFinalizerStorage{LocalStorage: raw, entered: make(chan struct{}), release: make(chan struct{})}
	h := newHarnessServiceWithOpts(t, edge.URL(), harnessOpts{inheritedStorage: held, maxConcurrent: 1})
	s := h.svc
	var observed atomic.Pointer[provider.Stream]
	s.observe = func(context.Context, string) (*provider.Stream, error) { return observed.Load(), nil }
	s.cfg.App.Download.StreamerRestartWaitSeconds = 120
	var millis atomic.Int64
	origin := time.Now().UTC().Truncate(time.Millisecond)
	millis.Store(origin.UnixMilli())
	s.now = func() time.Time { return time.UnixMilli(millis.Load()).UTC() }
	t.Cleanup(s.Shutdown)
	seedArchiveChannel(t, h.repo, "manual-channel")
	firstID := "broadcast-first"
	observed.Store(&provider.Stream{ID: firstID, UserID: "manual-channel", StartedAt: origin})
	job, err := s.Start(t.Context(), Params{BroadcasterID: "manual-channel", BroadcasterLogin: "channel", DisplayName: "Channel", Quality: repository.QualityHigh, StreamID: &firstID, StreamStartedAt: origin})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-held.entered:
	case <-time.After(20 * time.Second):
		t.Fatal("first finalizer never started")
	}
	first, err := h.repo.GetVideoByJobID(t.Context(), job)
	if err != nil {
		t.Fatal(err)
	}
	intent := waitManualIntent(t, h.repo, job, func(i *repository.RecordingIntent) bool { return i.Status == "waiting" })
	if intent.WaitUntil == nil || !intent.WaitUntil.Equal(origin.Add(120*time.Second)) {
		t.Fatalf("initial deadline: %+v", intent)
	}
	millis.Store(origin.Add(100 * time.Second).UnixMilli())
	returned := provider.Stream{ID: "broadcast-second", UserID: "manual-channel", Title: "Second broadcast", StartedAt: s.now()}
	observed.Store(&returned)
	s.ObserveStreamOnline(returned)
	second := waitManualIntent(t, h.repo, job, func(i *repository.RecordingIntent) bool { return i.CurrentJobID != job && i.Status == "waiting" })
	secondVideo, err := h.repo.GetVideoByJobID(t.Context(), second.CurrentJobID)
	if err != nil {
		t.Fatal(err)
	}
	waitForVideoStatus(t, h.repo, secondVideo.ID, repository.VideoStatusDone, 20*time.Second)
	if !second.WaitUntil.Equal(origin.Add(220 * time.Second)) {
		t.Fatalf("second stop did not get a full window: %+v", second)
	}
	if v, _ := h.repo.GetVideo(t.Context(), first.ID); v.Status != repository.VideoStatusRunning {
		t.Fatalf("first finalizer was not still owned: %+v", v)
	}
	if s.work.Used("live") != 1 {
		t.Fatalf("continuation consumed %d live slots", s.work.Used("live"))
	}
	if _, err := s.Start(t.Context(), Params{BroadcasterID: "manual-channel"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("waiting intent admitted duplicate: %v", err)
	}
	seedArchiveChannel(t, h.repo, "unrelated-channel")
	if _, err := s.Start(t.Context(), Params{BroadcasterID: "unrelated-channel"}); !errors.Is(err, ErrAtCapacity) {
		t.Fatalf("waiting intent lost its slot to another channel: %v", err)
	}
	s.ObserveStreamOnline(returned) // Duplicate broadcast must never become another member.
	// A second return is discovered by polling alone, within a fresh window.
	millis.Store(origin.Add(200 * time.Second).UnixMilli())
	thirdStream := provider.Stream{ID: "broadcast-third", UserID: "manual-channel", Title: "Third broadcast", StartedAt: s.now()}
	observed.Store(&thirdStream)
	third := waitManualIntent(t, h.repo, job, func(i *repository.RecordingIntent) bool {
		return i.CurrentJobID != second.CurrentJobID && i.Status == "waiting"
	})
	thirdVideo, err := h.repo.GetVideoByJobID(t.Context(), third.CurrentJobID)
	if err != nil {
		t.Fatal(err)
	}
	waitForVideoStatus(t, h.repo, thirdVideo.ID, repository.VideoStatusDone, 20*time.Second)
	if !third.WaitUntil.Equal(origin.Add(320*time.Second)) || s.work.Used("live") != 1 {
		t.Fatalf("third capture lost the fresh window or shared slot: %+v", third)
	}
	close(held.release)
	waitForVideoStatus(t, h.repo, first.ID, repository.VideoStatusDone, 20*time.Second)
	rows, err := h.repo.ListRelatedRecordings(t.Context(), first.ID)
	if err != nil || len(rows) != 3 || rows[0].ID != first.ID || rows[1].ID != secondVideo.ID || rows[2].ID != thirdVideo.ID {
		t.Fatalf("chain=%+v err=%v", rows, err)
	}
	// Stop through the old completed recording, while the latest waits.
	s.Cancel(job)
	waitManualIntent(t, h.repo, job, func(i *repository.RecordingIntent) bool { return i.Status == "stopped" })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := s.work.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
	if s.work.Used("live") != 0 {
		t.Fatal("stopped intent leaked live reservation")
	}
}

func waitManualIntent(t *testing.T, repo repository.Repository, id string, predicate func(*repository.RecordingIntent) bool) *repository.RecordingIntent {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		i, err := repo.GetRecordingIntent(t.Context(), id)
		if err == nil && predicate(i) {
			return i
		}
		if time.Now().After(deadline) {
			t.Fatalf("intent did not converge: %+v error=%v", i, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestRecoveryFinalizesSavedMediaWithoutFetchingAnotherBroadcast(t *testing.T) {
	for _, scenario := range []struct {
		known        bool
		pendingSplit bool
	}{{true, false}, {false, false}, {true, true}, {false, true}} {
		t.Run(fmt.Sprintf("known=%v/pending_split=%v", scenario.known, scenario.pendingSplit), func(t *testing.T) {
			known := scenario.known
			requireFFmpegHarness(t)
			edge := newTwitchEdge(t, twitchEdgeOpts{tsCount: 8, windowA: 8, baseSeqA: 100})
			h := newHarnessService(t, edge.URL())
			seedArchiveChannel(t, h.repo, "identity-channel")
			if _, err := h.repo.UpsertUser(t.Context(), &repository.User{ID: "identity-owner", Login: "identityowner", DisplayName: "Owner", Role: "owner"}); err != nil {
				t.Fatal(err)
			}
			schedule, err := h.repo.CreateSchedule(t.Context(), &repository.ScheduleInput{BroadcasterID: "identity-channel", RequestedBy: "identity-owner", Quality: repository.QualityHigh})
			if err != nil {
				t.Fatal(err)
			}
			var identity *string
			if known {
				identity = ptr.StringOrNil(harnessBroadcastID)
			}
			jobID, err := h.svc.Start(t.Context(), Params{BroadcasterID: "identity-channel", BroadcasterLogin: "channel", DisplayName: "Channel", Quality: repository.QualityHigh, StreamID: identity, TriggerScheduleID: &schedule.ID})
			if err != nil {
				t.Fatal(err)
			}
			waitUntil(t, "saved capture before shutdown", func() bool {
				files, err := filepath.Glob(filepath.Join(h.scratchDir, jobID, "part01", "segments", "*.ts"))
				return err == nil && len(files) >= 4
			})
			h.svc.Shutdown()
			job, err := h.repo.GetJob(t.Context(), jobID)
			if err != nil {
				t.Fatal(err)
			}
			// Simulate death while fetching, before any clean-shutdown preparation.
			state, err := UnmarshalResumeState(job.ResumeState)
			if err != nil {
				t.Fatal(err)
			}
			state.SetStage(StageSegments)
			if scenario.pendingSplit {
				state.SetStage(StagePrepareInput)
				state.PendingSplit = true
			}
			checkpoint, err := state.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h.db.ExecContext(t.Context(), "UPDATE jobs SET resume_state=? WHERE id=?", string(checkpoint), jobID); err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int64
			newBroadcast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.Error(w, "new broadcast has reset sequence zero", http.StatusNotFound)
			}))
			defer newBroadcast.Close()
			restarted := resumeOver(t, h, newBroadcast.URL)
			defer restarted.svc.Shutdown()
			restarted.svc.observe = func(context.Context, string) (*provider.Stream, error) {
				if scenario.pendingSplit {
					parts, err := h.repo.ListVideoParts(t.Context(), job.VideoID)
					if err != nil || len(parts) != 1 || parts[0].SizeBytes <= 0 {
						t.Errorf("identity lookup preceded saved part finalization: %+v %v", parts, err)
					}
					current, err := h.repo.GetJob(t.Context(), jobID)
					if err != nil || current.AcceptsMetadata {
						t.Errorf("prepared recovery accepted live metadata before identity verification: %+v %v", current, err)
					}
				}
				return &provider.Stream{ID: "different-broadcast", UserID: "identity-channel", StartedAt: time.Now()}, nil
			}
			if err := restarted.svc.Resume(t.Context()); err != nil {
				t.Fatal(err)
			}
			video := waitForVideoStatus(t, h.repo, job.VideoID, repository.VideoStatusDone, 30*time.Second)
			if requests.Load() != 0 {
				t.Fatalf("saved media recovery fetched the replacement broadcast %d times", requests.Load())
			}
			if known && (video.StreamID == nil || *video.StreamID != *identity) {
				t.Fatal("original broadcast was relabeled")
			}
			if !known && video.StreamID != nil {
				t.Fatal("current broadcast was used to guess missing identity")
			}
			parts, err := h.repo.ListVideoParts(t.Context(), video.ID)
			if err != nil || len(parts) != 1 || parts[0].SizeBytes <= 0 || parts[0].StartMediaSeq != 100 {
				t.Fatalf("saved capture lost: %+v %v", parts, err)
			}
			if video.CompletionKind != repository.CompletionKindPartial || !video.Truncated {
				t.Fatalf("unfinished captured broadcast reported complete: %+v", video)
			}
		})
	}
}

func TestManualShutdownPreservesWaitingIntentAndPreparedFinalizer(t *testing.T) {
	requireFFmpegHarness(t)
	edge := newTwitchEdge(t, twitchEdgeOpts{tsCount: 3, windowA: 3, baseSeqA: 100, aEndlist: 3})
	raw, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	held := &heldFinalizerStorage{LocalStorage: raw, entered: make(chan struct{}), release: make(chan struct{})}
	h := newHarnessServiceWithOpts(t, edge.URL(), harnessOpts{inheritedStorage: held})
	h.svc.cfg.App.Download.StreamerRestartWaitSeconds = 120
	origin := time.Now().UTC().Truncate(time.Millisecond)
	h.svc.now = func() time.Time { return origin }
	seedArchiveChannel(t, h.repo, "shutdown-manual")
	jobID, err := h.svc.Start(t.Context(), Params{BroadcasterID: "shutdown-manual", BroadcasterLogin: "channel", DisplayName: "Channel", Quality: repository.QualityHigh, StreamID: ptr.StringOrNil(harnessBroadcastID)})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-held.entered:
	case <-time.After(20 * time.Second):
		t.Fatal("finalizer never started")
	}
	waitManualIntent(t, h.repo, jobID, func(i *repository.RecordingIntent) bool { return i.Status == "waiting" })
	h.svc.Shutdown()
	intent, err := h.repo.GetRecordingIntent(t.Context(), jobID)
	if err != nil || intent.Status != "waiting" || intent.StopRequested || !intent.WaitUntil.Equal(origin.Add(120*time.Second)) {
		t.Fatalf("shutdown stopped manual intent: %+v %v", intent, err)
	}
	job, err := h.repo.GetJob(t.Context(), jobID)
	if err != nil || job.Status != repository.JobStatusRunning {
		t.Fatalf("shutdown failed recoverable finalizer: %+v %v", job, err)
	}
	restarted := resumeOver(t, h, edge.URL())
	defer restarted.svc.Shutdown()
	restarted.svc.now = func() time.Time { return origin.Add(30 * time.Second) }
	if err := restarted.svc.Resume(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitForVideoStatus(t, h.repo, job.VideoID, repository.VideoStatusDone, 20*time.Second)
	restored := waitManualIntent(t, h.repo, jobID, func(i *repository.RecordingIntent) bool { return i.Status == "waiting" })
	if !restored.WaitUntil.Equal(*intent.WaitUntil) || restarted.svc.work.Used("live") != 1 {
		t.Fatalf("restart lost waiting ownership: %+v", restored)
	}
	restarted.svc.Cancel(jobID)
}
