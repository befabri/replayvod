package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	provider "github.com/befabri/replayvod/server/internal/twitch"
)

func TestKnownIdentityRecoveryMatchesOrDefersWithoutChangingCapture(t *testing.T) {
	for _, providerFailed := range []bool{false, true} {
		t.Run(map[bool]string{false: "matching broadcast", true: "provider failure"}[providerFailed], func(t *testing.T) {
			s := newTestService(t, t.TempDir())
			defer s.Shutdown()
			d := &download{recovered: true, resume: NewResumeState()}
			d.resume.SetStage(StageSegments)
			d.resume.StartPart(0)
			d.resume.NoteCommitted(0)
			type checkpoint ResumeState // Compare without MarshalJSON touching CheckpointAt.
			before := checkpoint(*d.resume)
			id := "original"
			failure := errors.New("provider unavailable")
			calls := 0
			s.observe = func(ctx context.Context, broadcasterID string) (*provider.Stream, error) {
				calls++
				if _, bounded := ctx.Deadline(); !bounded || broadcasterID != "channel" {
					t.Error("identity lookup has no bound or wrong broadcaster")
				}
				if providerFailed {
					return nil, failure
				}
				return &provider.Stream{ID: id}, nil
			}
			err := s.recoverCaptureIdentity(t.Context(), d, Params{BroadcasterID: "channel", StreamID: &id})
			if providerFailed && !errors.Is(err, failure) || !providerFailed && err != nil {
				t.Fatalf("identity result = %v", err)
			}
			if !reflect.DeepEqual(before, checkpoint(*d.resume)) || d.captureIdentityVerified == providerFailed || calls != 1 {
				t.Fatalf("identity check changed saved capture or verification: %+v, verified=%v, calls=%d", d.resume, d.captureIdentityVerified, calls)
			}
			if !providerFailed {
				if err := s.recoverCaptureIdentity(t.Context(), d, Params{}); err != nil || calls != 1 {
					t.Fatal("verified capture repeated identity lookup")
				}
			}
		})
	}
}

func TestCaptureWithoutIdentityRequiresLaterBroadcastStart(t *testing.T) {
	s := newTestService(t, t.TempDir())
	defer s.Shutdown()
	at := time.Now().UTC()
	d := &download{startedAt: at}
	for _, cause := range []error{ErrVariantChanged, &playbackResolutionError{cause: errors.New("manifest unavailable")}} {
		for _, offset := range []time.Duration{-time.Second, 0, time.Second} {
			s.observe = func(context.Context, string) (*provider.Stream, error) {
				return &provider.Stream{ID: "observed", StartedAt: at.Add(offset)}, nil
			}
			if ended := s.liveCaptureEnded(t.Context(), d, Params{BroadcasterID: "channel"}, cause); ended != (offset > 0) {
				t.Fatalf("offset=%s cause=%v ended=%v", offset, cause, ended)
			}
		}
	}
}

func TestPendingKnownBroadcastCannotRecoverIntoReplacement(t *testing.T) {
	s := newTestService(t, t.TempDir())
	defer s.Shutdown()
	original := "obsolete"
	s.observe = func(context.Context, string) (*provider.Stream, error) {
		return &provider.Stream{ID: "current"}, nil
	}
	d := &download{recovered: true, resume: NewResumeState()}
	if err := s.recoverCaptureIdentity(t.Context(), d, Params{BroadcasterID: "channel", StreamID: &original}); err != nil {
		t.Fatal(err)
	}
	if !shouldSkipSegmentFetch(d.resume) || d.captureIdentityVerified || !d.claim().MetadataStopped {
		t.Fatalf("empty checkpoint allowed capture under obsolete identity: %+v", d.resume)
	}
}

func TestRestartSuccessorSeedsItsOwnOpeningMetadata(t *testing.T) {
	s := newTestService(t, t.TempDir())
	defer s.Shutdown()
	s.hydrator = streammeta.NewHydrator(s.repo, nil, streammeta.Config{}, discardLog())
	ctx := t.Context()
	if _, err := s.repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "metadata", BroadcasterLogin: "metadata", BroadcasterName: "Metadata"}); err != nil {
		t.Fatal(err)
	}
	p := Params{BroadcasterID: "metadata", BroadcasterLogin: "metadata", DisplayName: "Metadata", Quality: repository.QualityHigh, Title: "First title", CategoryID: "first-category", CategoryName: "First category"}
	params, _ := json.Marshal(p)
	checkpoint, _ := NewResumeState().MarshalJSON()
	first, err := repository.CreateAttempt(ctx, s.repo, &repository.VideoInput{JobID: "initial", Filename: "initial", BroadcasterID: p.BroadcasterID, DisplayName: p.DisplayName, Quality: p.Quality, Status: repository.VideoStatusPending, IntentID: "intent", IntentParams: params, RestartWaitSeconds: 120}, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	s.linkInitialMetadata(ctx, first.ID, p)
	now := time.Now().UTC()
	if err := s.repo.SetRecordingIntentWaiting(ctx, "intent", first.JobID, now.Add(120*time.Second)); err != nil {
		t.Fatal(err)
	}
	intent, err := s.repo.GetRecordingIntent(ctx, "intent")
	if err != nil {
		t.Fatal(err)
	}
	stream := provider.Stream{ID: "returned", UserID: p.BroadcasterID, Title: "Return title", GameID: "return-category", GameName: "Return category", StartedAt: now}
	lookups := 0
	s.observe = func(ctx context.Context, id string) (*provider.Stream, error) {
		lookups++
		if _, bounded := ctx.Deadline(); !bounded || id != p.BroadcasterID {
			t.Error("unbounded or misdirected successor enrichment")
		}
		return &stream, nil
	}
	// EventSub supplies identity; enrich it before claiming the new attempt.
	observed := provider.Stream{ID: stream.ID, UserID: stream.UserID, StartedAt: stream.StartedAt}
	bus := eventbus.New()
	s.SetEventBus(bus)
	changes := bus.VideoChanges.Subscribe(ctx)
	d, _, _, err := s.admitSuccessor(ctx, &manualRun{id: "intent", params: p}, intent, onlineObservation{stream: observed, at: now})
	if err != nil {
		t.Fatal(err)
	}
	if lookups != 1 {
		t.Fatalf("successor enrichment calls = %d", lookups)
	}
	recvVideoChange(t, changes)
	rows, err := s.repo.ListRelatedRecordings(ctx, first.ID)
	if err != nil || len(rows) != 2 || rows[1].ID != d.videoID {
		t.Fatalf("successor notification preceded committed relationship: %+v, %v", rows, err)
	}
	for _, want := range []struct {
		id              int64
		title, category string
	}{{first.ID, p.Title, p.CategoryID}, {d.videoID, stream.Title, stream.GameID}} {
		rows, err := s.repo.ListVideoMetadataChanges(ctx, want.id)
		if err != nil || len(rows) != 1 || rows[0].Title == nil || rows[0].Title.Name != want.title || rows[0].Category == nil || rows[0].Category.ID != want.category {
			t.Fatalf("opening metadata for %d = %+v, %v", want.id, rows, err)
		}
	}
}
