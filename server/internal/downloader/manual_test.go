package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/ptr"
	"github.com/befabri/replayvod/server/internal/repository"
	provider "github.com/befabri/replayvod/server/internal/twitch"
)

func TestManualIntentRestoresOriginalDeadlineAndPublishesTransitions(t *testing.T) {
	for _, terminal := range []string{"expired", "stopped"} {
		t.Run(terminal, func(t *testing.T) {
			s := newTestService(t, t.TempDir())
			d := seedWebhookAttempt(t, s, "waiting-intent")
			v, _ := s.repo.GetVideo(t.Context(), d.videoID)
			d.broadcasterID = v.BroadcasterID
			ctx := t.Context()
			now := time.Now().UTC().Truncate(time.Millisecond)
			params, _ := json.Marshal(Params{BroadcasterID: d.broadcasterID, Quality: repository.QualityHigh})
			if err := s.repo.CreateRecordingIntent(ctx, repository.RecordingIntent{ID: d.jobID, BroadcasterID: d.broadcasterID, Params: params, WaitSeconds: 120, CurrentJobID: d.jobID, LastStreamID: "first"}); err != nil {
				t.Fatal(err)
			}
			if err := s.repo.LinkRecordingIntentVideo(ctx, d.jobID, d.videoID, nil); err != nil {
				t.Fatal(err)
			}
			d.resume.CaptureStoppedAt = &now
			checkpoint, err := d.resume.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if err := s.repo.CheckpointAttempt(ctx, d.jobID, d.executionID, checkpoint); err != nil {
				t.Fatal(err)
			}
			initialBus := eventbus.New()
			s.SetEventBus(initialBus)
			waiting := initialBus.VideoChanges.Subscribe(ctx)
			if err := s.repo.MarkVideoDone(ctx, d.videoID, 1, 10, nil, repository.CompletionKindComplete, false); err != nil {
				t.Fatal(err)
			}
			if err := s.repo.MarkJobDone(ctx, d.jobID); err != nil {
				t.Fatal(err)
			}
			s.now = func() time.Time { return now.Add(30 * time.Second) }
			if err := s.resumeManualIntents(ctx); err != nil {
				t.Fatal(err)
			}
			recvVideoChange(t, waiting)
			waitUntil(t, "waiting reservation", func() bool { return len(s.ListActiveProgress()) == 1 })
			s.Shutdown()
			prior, err := s.repo.GetRecordingIntent(ctx, d.jobID)
			if err != nil || prior.Status != "waiting" || !prior.WaitUntil.Equal(now.Add(120*time.Second)) {
				t.Fatalf("shutdown changed deadline: %+v %v", prior, err)
			}
			next := NewService(s.cfg, s.repo, s.storage, nil, nil, nil, discardLog())
			defer next.Shutdown()
			bus := eventbus.New()
			next.SetEventBus(bus)
			changes := bus.VideoChanges.Subscribe(ctx)
			var clock atomic.Int64
			clock.Store(now.Add(119 * time.Second).UnixMilli())
			next.now = func() time.Time { return time.UnixMilli(clock.Load()).UTC() }
			if err := next.resumeManualIntents(ctx); err != nil {
				t.Fatal(err)
			}
			waitUntil(t, "restored reservation", func() bool { return len(next.ListActiveProgress()) == 1 })
			if terminal == "expired" {
				clock.Store(now.Add(121 * time.Second).UnixMilli())
			} else if err := next.Cancel(d.jobID); err != nil {
				t.Fatal(err)
			}
			waitUntil(t, "intent closure", func() bool {
				intent, _ := next.repo.GetRecordingIntent(ctx, d.jobID)
				return intent.Status == terminal
			})
			recvVideoChange(t, changes)
			waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			if err := next.work.WaitIdle(waitCtx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUnknownBroadcastRecoverySealsSavedCapture(t *testing.T) {
	s := newTestService(t, t.TempDir())
	d := seedWebhookAttempt(t, s, "unknown-broadcast")
	d.recovered = true
	d.runCtx = t.Context()
	d.resume.StartPart(0)
	d.resume.NoteCommitted(0)
	d.resume.SetStage(StageSegments)
	if err := s.recoverCaptureIdentity(t.Context(), d, Params{}); err != nil {
		t.Fatal(err)
	}
	if d.resume.Stage != StagePrepareInput || d.resume.CaptureStoppedAt == nil || !d.resume.HadWindowRoll {
		t.Fatalf("unsafe recovered capture: %+v", d.resume)
	}
	v, err := s.repo.GetVideo(t.Context(), d.videoID)
	if err != nil || v.StreamID != nil {
		t.Fatalf("unknown original identity changed: %+v %v", v, err)
	}
	if !d.claim().MetadataStopped {
		t.Fatal("recovered capture would reopen live metadata at claim")
	}
}

type refusedManualStop struct{ repository.Repository }

func (refusedManualStop) RequestRecordingIntentStop(context.Context, string) error {
	return errors.New("database unavailable")
}

func TestManualStopAcknowledgesOnlyPersistedRequest(t *testing.T) {
	s := newTestService(t, t.TempDir())
	d := seedWebhookAttempt(t, s, "stop-intent")
	v, err := s.repo.GetVideo(t.Context(), d.videoID)
	if err != nil {
		t.Fatal(err)
	}
	intent := repository.RecordingIntent{ID: d.jobID, BroadcasterID: v.BroadcasterID, Params: []byte(`{}`), WaitSeconds: 120, CurrentJobID: d.jobID}
	if err := s.repo.CreateRecordingIntent(t.Context(), intent); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.LinkRecordingIntentVideo(t.Context(), intent.ID, v.ID, nil); err != nil {
		t.Fatal(err)
	}
	repo := s.repo
	s.repo = refusedManualStop{repo}
	if err := s.Cancel(d.jobID); err == nil {
		t.Fatal("Stop acknowledged a failed persistence operation")
	}
	fresh, err := repo.GetRecordingIntent(t.Context(), intent.ID)
	if err != nil || fresh.StopRequested {
		t.Fatalf("failed stop changed the intent: %+v %v", fresh, err)
	}
	s.repo = repo
	if err := s.Cancel(d.jobID); err != nil {
		t.Fatal(err)
	}
	fresh, err = repo.GetRecordingIntent(t.Context(), intent.ID)
	if err != nil || !fresh.StopRequested {
		t.Fatalf("acknowledged Stop was not durable: %+v %v", fresh, err)
	}
}

func TestVanishedPlaylistRequiresAuthoritativeStopObservation(t *testing.T) {
	s := newTestService(t, t.TempDir())
	d := &download{startedAt: time.Now().Add(-time.Minute)}
	params := Params{BroadcasterID: "channel", StreamID: ptr.StringOrNil("first")}
	for _, tc := range []struct {
		name   string
		stream *provider.Stream
		err    error
		want   bool
	}{
		{"same broadcast", &provider.Stream{ID: "first"}, nil, false},
		{"different broadcast", &provider.Stream{ID: "second"}, nil, true},
		{"offline", nil, nil, true},
		{"provider outage", nil, errors.New("provider unavailable"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.observe = func(context.Context, string) (*provider.Stream, error) { return tc.stream, tc.err }
			if got := s.liveCaptureEnded(t.Context(), d, params, ErrVariantChanged); got != tc.want {
				t.Fatalf("capture ended=%v, want %v", got, tc.want)
			}
		})
	}
}

type deferredManualVideo struct {
	repository.Repository
	id          int64
	unavailable atomic.Bool
}

func (r *deferredManualVideo) GetVideo(ctx context.Context, id int64) (*repository.Video, error) {
	if id == r.id && r.unavailable.Load() {
		return nil, errors.New("temporary video read failure")
	}
	return r.Repository.GetVideo(ctx, id)
}

func TestManualRecoveryIsolatesUnreadableCheckpointAndHonorsStop(t *testing.T) {
	for _, name := range []string{"active intent", "stop persisted before restart", "deferred child retries without an event"} {
		stopped := name == "stop persisted before restart"
		t.Run(name, func(t *testing.T) {
			s := newTestService(t, t.TempDir())
			defer s.Shutdown()
			ctx := t.Context()
			if _, err := s.repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "manual-recovery", BroadcasterLogin: "manual-recovery", BroadcasterName: "Manual"}); err != nil {
				t.Fatal(err)
			}
			params, err := json.Marshal(Params{BroadcasterID: "manual-recovery", BroadcasterLogin: "manual-recovery", Quality: repository.QualityHigh})
			if err != nil {
				t.Fatal(err)
			}
			input := &repository.VideoInput{JobID: "a-unreadable", Filename: "a-unreadable", BroadcasterID: "manual-recovery", DisplayName: "Manual", Quality: repository.QualityHigh, Status: repository.VideoStatusPending, IntentID: "manual-recovery", IntentParams: params, RestartWaitSeconds: 120}
			bad, err := repository.CreateAttempt(ctx, s.repo, input, json.RawMessage(`{"stage":"SEGMENTS","current_part_index":"unreadable"}`))
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			if err := s.repo.SetRecordingIntentWaiting(ctx, input.IntentID, input.JobID, now.Add(120*time.Second)); err != nil {
				t.Fatal(err)
			}
			state := NewResumeState()
			state.SetStage(StagePrepareInput)
			state.CaptureStoppedAt = &now
			checkpoint, err := state.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			input.IntentPreviousJobID, input.JobID, input.Filename = input.JobID, "b-recoverable", "b-recoverable"
			input.IntentObservedAt = now
			good, err := repository.CreateAttempt(ctx, s.repo, input, checkpoint)
			if err != nil {
				t.Fatal(err)
			}
			var releaseCapacity []func()
			if stopped {
				if err := s.Cancel(bad.JobID); err != nil {
					t.Fatal(err)
				}
				for _, key := range []string{"another-channel", "last-channel"} {
					reservation, err := s.work.Reserve("live", key)
					if err != nil {
						t.Fatal(err)
					}
					defer reservation.Release()
					releaseCapacity = append(releaseCapacity, reservation.Release)
				}
			}
			fault := &deferredManualVideo{Repository: s.repo, id: bad.ID}
			if name == "deferred child retries without an event" {
				fault.unavailable.Store(true)
				s.repo = fault
				s.retryInterval = 20 * time.Millisecond
			}
			if err := s.resumeManualIntents(ctx); err != nil {
				t.Fatal(err)
			}
			waitUntil(t, "healthy sibling recovery", func() bool {
				job, err := s.repo.GetJob(ctx, good.JobID)
				if stopped {
					return err == nil && job.Status == repository.JobStatusFailed && job.ExecutionID == ""
				}
				return err == nil && job.ExecutionID != ""
			})
			if fault.unavailable.Load() {
				s.mu.Lock()
				owner := s.manual[input.IntentID]
				s.mu.Unlock()
				if owner == nil {
					t.Fatal("transient child failure discarded the intent owner")
				}
				fault.unavailable.Store(false)
				waitUntil(t, "deferred child rediscovered", func() bool {
					row, err := s.repo.GetVideo(ctx, bad.ID)
					return err == nil && row.Status == repository.VideoStatusFailed
				})
				s.mu.Lock()
				same := s.manual[input.IntentID] == owner
				s.mu.Unlock()
				if !same {
					t.Fatal("child retry replaced the intent owner")
				}
			}
			failed, err := s.repo.GetVideo(ctx, bad.ID)
			if err != nil || failed.Status != repository.VideoStatusFailed {
				t.Fatalf("unreadable child left recoverable: %+v, %v", failed, err)
			}
			if stopped && failed.CompletionKind != repository.CompletionKindCancelled {
				t.Fatalf("persisted Stop lost cancellation outcome: %+v", failed)
			}
			if err := s.Cancel(good.JobID); err != nil {
				t.Fatal(err)
			}
			for _, release := range releaseCapacity {
				release()
			}
			waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			if err := s.work.WaitIdle(waitCtx); err != nil {
				t.Fatal(err)
			}
			jobs, err := s.repo.ListRecordingIntentJobs(ctx, input.IntentID, "", 100)
			if err != nil || len(jobs) != 0 {
				t.Fatalf("Stop stranded unfinished siblings: %+v, %v", jobs, err)
			}
		})
	}
}
