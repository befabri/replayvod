//go:build ffmpeg

package downloader

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	provider "github.com/befabri/replayvod/server/internal/twitch"
)

func TestDelayedManualReturnFinalizesOnlyCurrentBroadcastMedia(t *testing.T) {
	requireFFmpegHarness(t)
	edge := newTwitchEdge(t, twitchEdgeOpts{tsCount: 3, windowA: 3, baseSeqA: 100, aEndlist: 3})
	h := newHarnessServiceWithOpts(t, edge.URL(), harnessOpts{maxConcurrent: 1})
	s, ctx := h.svc, t.Context()
	t.Cleanup(s.Shutdown)
	seedArchiveChannel(t, h.repo, "manual-channel")
	origin := time.Now().UTC().Truncate(time.Millisecond)
	s.now = func() time.Time { return origin.Add(121 * time.Second) }
	p := Params{BroadcasterID: "manual-channel", BroadcasterLogin: "channel", Quality: repository.QualityHigh}
	params, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	firstID := "broadcast-a"
	first, err := repository.CreateAttempt(ctx, h.repo, &repository.VideoInput{
		JobID: "first", Filename: "first", BroadcasterID: p.BroadcasterID, Quality: p.Quality,
		Status: repository.VideoStatusPending, StreamID: &firstID,
		IntentID: "first", IntentParams: params, RestartWaitSeconds: 120,
	}, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.MarkVideoDone(ctx, first.ID, 1, 1, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.MarkJobDone(ctx, first.JobID); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.SetRecordingIntentWaiting(ctx, "first", "first", origin.Add(120*time.Second)); err != nil {
		t.Fatal(err)
	}
	current := provider.Stream{ID: "broadcast-c", UserID: p.BroadcasterID, Title: "C", GameID: "category", Language: "en", StartedAt: origin.Add(119 * time.Second)}
	s.observe = func(context.Context, string) (*provider.Stream, error) { return &current, nil }
	barrier := &manualReadBarrier{Repository: h.repo, entered: make(chan struct{}), release: make(chan struct{})}
	s.repo = barrier
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(barrier.release) }) })
	if err := s.resumeManualIntents(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-barrier.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("manual owner did not reach persistence barrier")
	}
	s.mu.Lock()
	m := s.manual["first"]
	s.mu.Unlock()
	obsolete := current
	obsolete.ID, obsolete.Title = "broadcast-b", "B"
	m.observeOnline(onlineObservation{stream: obsolete, at: origin.Add(118 * time.Second)})
	m.observeOnline(onlineObservation{stream: current, at: origin.Add(119 * time.Second)})
	s.ObserveStreamOnline(current) // Late duplicate must retain C's on-time observation.
	release.Do(func() { close(barrier.release) })
	intent := waitManualIntent(t, h.repo, "first", func(i *repository.RecordingIntent) bool { return i.CurrentJobID != "first" })
	v, err := h.repo.GetVideoByJobID(ctx, intent.CurrentJobID)
	if err != nil {
		t.Fatal(err)
	}
	waitForVideoStatus(t, h.repo, v.ID, repository.VideoStatusDone, 20*time.Second)
	v, err = h.repo.GetVideo(ctx, v.ID)
	if err != nil || v.StreamID == nil || *v.StreamID != current.ID || v.SizeBytes == nil || *v.SizeBytes == 0 {
		t.Fatalf("current media finalized under an obsolete identity: %+v, %v", v, err)
	}
	parts, err := h.repo.ListVideoParts(ctx, v.ID)
	if err != nil || len(parts) != 1 || parts[0].SizeBytes == 0 {
		t.Fatalf("current capture did not pass through media finalization: %+v, %v", parts, err)
	}
	if err := s.Cancel("first"); err != nil {
		t.Fatal(err)
	}
}

func TestBroadcastChangeDuringPlaybackResolutionCannotCapture(t *testing.T) {
	requireFFmpegHarness(t)
	edge := newTwitchEdge(t, twitchEdgeOpts{tsCount: 3, windowA: 3, baseSeqA: 100, aEndlist: 3})
	release := edge.BlockGQL()
	defer release()
	h := newHarnessServiceWithOpts(t, edge.URL(), harnessOpts{maxConcurrent: 1})
	s := h.svc
	t.Cleanup(s.Shutdown)
	seedArchiveChannel(t, h.repo, "channel")
	original := "broadcast-b"
	var current atomic.Pointer[provider.Stream]
	current.Store(&provider.Stream{ID: original})
	s.observe = func(context.Context, string) (*provider.Stream, error) { return current.Load(), nil }
	id, err := s.Start(t.Context(), Params{BroadcasterID: "channel", BroadcasterLogin: "channel", StreamID: &original, Quality: repository.QualityHigh})
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "blocked playback request", func() bool { return edge.LastGQLVars() != nil })
	current.Store(&provider.Stream{ID: "broadcast-c"})
	release()
	v, err := h.repo.GetVideoByJobID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	waitForVideoStatus(t, h.repo, v.ID, repository.VideoStatusFailed, 20*time.Second)
	parts, err := h.repo.ListVideoParts(t.Context(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range parts {
		if part.SizeBytes != 0 {
			t.Fatalf("obsolete admission captured current media: %+v", part)
		}
	}
	edge.mu.Lock()
	captured := edge.aCursor
	edge.mu.Unlock()
	if captured != 0 {
		t.Fatalf("obsolete admission polled current media: cursor=%d", captured)
	}
}
