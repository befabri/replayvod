package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/ptr"
	"github.com/befabri/replayvod/server/internal/repository"
	provider "github.com/befabri/replayvod/server/internal/twitch"
)

func TestManualObservationsPreserveReturnAtDeadline(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	deadline := start.Add(120 * time.Second)
	observation := func(id string, second int) onlineObservation {
		return onlineObservation{stream: provider.Stream{ID: id, StartedAt: start.Add(time.Second)}, at: start.Add(time.Duration(second) * time.Second)}
	}
	for _, tc := range []struct {
		name       string
		events     []onlineObservation
		wantID     string
		wantSecond int
	}{
		{"late duplicate", []onlineObservation{observation("return", 119), observation("return", 121)}, "return", 119},
		{"deadline inclusive", []onlineObservation{observation("return", 120), observation("return", 121)}, "return", 120},
		{"only late", []onlineObservation{observation("return", 121)}, "", 0},
		{"current broadcast duplicate", []onlineObservation{observation("return", 119), observation("original", 120)}, "return", 119},
		{"different returns", []onlineObservation{observation("return", 119), observation("later", 121)}, "return", 119},
		{"out of order", []onlineObservation{observation("return", 121), observation("return", 119)}, "return", 119},
		{"unknown identity ignored", []onlineObservation{observation("return", 119), observation("", 120)}, "return", 119},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &manualRun{online: make(chan struct{}, 1)}
			for _, observed := range tc.events {
				m.observeOnline(observed)
			}
			intent := &repository.RecordingIntent{LastStreamID: "original", WaitUntil: &deadline, CreatedAt: start}
			got, ok := m.nextReturn(intent)
			if ok != (tc.wantID != "") || got.stream.ID != tc.wantID || (ok && !got.at.Equal(start.Add(time.Duration(tc.wantSecond)*time.Second))) {
				t.Fatalf("return = %+v, %v; want %q at %ds", got, ok, tc.wantID, tc.wantSecond)
			}
			if _, ok := m.nextReturn(intent); ok {
				t.Fatal("observation consumed more than once")
			}
		})
	}
}

func TestManualUnknownBroadcastPreservesReturnProof(t *testing.T) {
	start := time.Now().UTC()
	deadline := start.Add(120 * time.Second)
	for _, newer := range []bool{false, true} {
		m := &manualRun{online: make(chan struct{}, 1)}
		started := start.Add(-time.Second)
		if newer {
			started = start.Add(time.Second)
		}
		m.observeOnline(onlineObservation{stream: provider.Stream{ID: "return", StartedAt: started}, at: start.Add(119 * time.Second)})
		m.observeOnline(onlineObservation{stream: provider.Stream{ID: "return"}, at: start.Add(121 * time.Second)})
		got, ok := m.nextReturn(&repository.RecordingIntent{CreatedAt: start, WaitUntil: &deadline})
		if ok != newer || (ok && !got.at.Equal(start.Add(119*time.Second))) {
			t.Fatalf("return proof lost or invented: %+v, %v (newer=%v)", got, ok, newer)
		}
	}
}

type manualReadBarrier struct {
	repository.Repository
	once             sync.Once
	entered, release chan struct{}
}

func (r *manualReadBarrier) GetRecordingIntent(ctx context.Context, id string) (*repository.RecordingIntent, error) {
	r.once.Do(func() {
		close(r.entered)
		select {
		case <-r.release:
		case <-ctx.Done():
		}
	})
	return r.Repository.GetRecordingIntent(ctx, id)
}

func TestDelayedManualOwnerAdmitsReturnDespiteLateDuplicate(t *testing.T) {
	testDelayedManualReturn(t, false, false)
}

func TestDelayedManualOwnerSkipsObsoleteBroadcast(t *testing.T) {
	testDelayedManualReturn(t, true, false)
}

func TestDelayedManualOwnerRetriesIdentityLookupAfterDeadline(t *testing.T) {
	testDelayedManualReturn(t, false, true)
}

func testDelayedManualReturn(t *testing.T, obsoleteFirst, failFirstLookup bool) {
	t.Helper()
	f := newArchiveFixture(t, 1, 1)
	releaseEdge := f.edge.hold()
	defer releaseEdge()
	s := f.svc
	returned := provider.Stream{ID: "return", UserID: "bc-1", Title: "Returned", GameID: "category", Language: "en"}
	var lookups atomic.Int64
	s.observe = func(context.Context, string) (*provider.Stream, error) {
		if lookups.Add(1) == 1 && failFirstLookup {
			return nil, errors.New("provider temporarily unavailable")
		}
		return &returned, nil
	}
	ctx := t.Context()
	start := time.Now().UTC().Truncate(time.Millisecond)
	params := Params{BroadcasterID: "bc-1", BroadcasterLogin: "bc-1", Quality: repository.QualityHigh}
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	input := &repository.VideoInput{JobID: "first", Filename: "first", BroadcasterID: "bc-1", Quality: repository.QualityHigh, Status: repository.VideoStatusPending, IntentID: "first", IntentParams: encoded, RestartWaitSeconds: 120, StreamID: ptr.StringOrNil("original")}
	v, err := repository.CreateAttempt(ctx, f.repo, input, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.repo.MarkVideoDone(ctx, v.ID, 1, 1, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.MarkJobDone(ctx, v.JobID); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.SetRecordingIntentWaiting(ctx, "first", "first", start.Add(120*time.Second)); err != nil {
		t.Fatal(err)
	}
	barrier := &manualReadBarrier{Repository: f.repo, entered: make(chan struct{}), release: make(chan struct{})}
	s.repo = barrier
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(barrier.release) }) })
	// The owner is blocked before it can process any online hint.
	s.now = func() time.Time { return start.Add(121 * time.Second) }
	if err := s.resumeManualIntents(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-barrier.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("intent owner did not reach read barrier")
	}
	s.mu.Lock()
	m := s.manual["first"]
	s.mu.Unlock()
	if obsoleteFirst {
		m.observeOnline(onlineObservation{stream: provider.Stream{ID: "obsolete", UserID: "bc-1", Title: "Obsolete", GameID: "category", Language: "en"}, at: start.Add(118 * time.Second)})
	}
	m.observeOnline(onlineObservation{stream: returned, at: start.Add(119 * time.Second)})
	s.ObserveStreamOnline(returned) // Duplicate at 121 seconds.
	release.Do(func() { close(barrier.release) })
	waitUntil(t, "on-time return admitted", func() bool {
		intent, err := f.repo.GetRecordingIntent(ctx, "first")
		return err == nil && intent.CurrentJobID != "first"
	})
	rows, err := f.repo.ListRelatedRecordings(ctx, v.ID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("return admissions = %+v, %v", rows, err)
	}
	intent, err := f.repo.GetRecordingIntent(ctx, "first")
	if err != nil {
		t.Fatal(err)
	}
	video, err := f.repo.GetVideoByJobID(ctx, intent.CurrentJobID)
	if err != nil || video.StreamID == nil || *video.StreamID != returned.ID {
		t.Fatalf("admitted obsolete broadcast for current playback: %+v, %v", video, err)
	}
	if err := s.Cancel("first"); err != nil {
		t.Fatal(err)
	}
}
