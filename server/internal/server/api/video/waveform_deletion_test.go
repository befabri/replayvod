package video

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/recordinglock"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/service/retention"
	"github.com/befabri/replayvod/server/internal/service/storagehealth"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/testdb"
)

func waveformDeletionFixture(t *testing.T, locks *recordinglock.Locks, wrap func(storage.Storage) storage.Storage) (repository.Repository, storage.Storage, *repository.Video, *retention.Service) {
	t.Helper()
	ctx := t.Context()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "audio", BroadcasterLogin: "audio", BroadcasterName: "Audio"}); err != nil {
		t.Fatal(err)
	}
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{JobID: "waveform-delete", Filename: "audio", DisplayName: "Audio", BroadcasterID: "audio", Status: repository.VideoStatusRunning, Quality: repository.QualityHigh, RecordingType: repository.RecordingTypeAudio})
	if err != nil {
		t.Fatal(err)
	}
	p, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{VideoID: v.ID, PartIndex: 1, Filename: "audio-part01.m4a", Quality: "audio_only", Codec: repository.CodecAAC, SegmentFormat: "fmp4"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.FinalizeVideoPart(ctx, &repository.VideoPartFinalize{ID: p.ID, DurationSeconds: 2, SizeBytes: 5, EndMediaSeq: 1}); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkVideoDone(ctx, v.ID, 2, 5, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, storagekeys.Video(p.Filename), strings.NewReader("audio")); err != nil {
		t.Fatal(err)
	}
	v, err = repo.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	monitor := storagehealth.New(repo, store, nil, testClientLogger(), "local", store.Root)
	if _, err := monitor.Attach(ctx); err != nil {
		t.Fatal(err)
	}
	var objects storage.Storage = store
	if wrap != nil {
		objects = wrap(store)
	}
	return repo, objects, v, retention.New(repo, streamMedia(t, repo, objects, monitor, locks), testClientLogger())
}

func TestAudioWaveformDoesNotRecreateDeletedRecording(t *testing.T) {
	for _, kind := range []string{repository.DeletionKindManual, repository.DeletionKindRetention} {
		t.Run(kind, func(t *testing.T) {
			locks := &recordinglock.Locks{}
			repo, store, v, deletions := waveformDeletionFixture(t, locks, nil)
			generator := &storageChangeWaveformGenerator{afterGenerate: func() {
				if err := deletions.DeleteRecording(t.Context(), v, kind); err != nil {
					t.Errorf("delete during generation: %v", err)
				}
			}}
			h := NewStreamHandler(repo, streamMedia(t, repo, store, nil, locks), nil, testClientLogger(), WithWaveformGenerator(generator))
			_, status, err := h.audioWaveform(t.Context(), v.ID)
			if err != nil || status != http.StatusGone {
				t.Errorf("waveform after deletion: status=%d err=%v, want 410", status, err)
			}
			fresh, err := repo.GetVideo(t.Context(), v.ID)
			if err != nil || fresh.DeletedAt == nil {
				t.Fatalf("recording deletion did not finish: %+v err=%v", fresh, err)
			}
			assertNoWaveformPublished(t, repo, v.ID)
		})
	}
}

type waveformSaveBarrier struct {
	storage.Storage
	entered chan struct{}
	release chan struct{}
}

func (s *waveformSaveBarrier) Save(ctx context.Context, path string, body io.Reader) error {
	close(s.entered)
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.Storage.Save(ctx, path, body)
}

func TestAudioWaveformPublicationSerializesWithPurge(t *testing.T) {
	locks := &recordinglock.Locks{}
	var barrier *waveformSaveBarrier
	repo, store, v, deletions := waveformDeletionFixture(t, locks, func(s storage.Storage) storage.Storage {
		barrier = &waveformSaveBarrier{Storage: s, entered: make(chan struct{}), release: make(chan struct{})}
		return barrier
	})
	h := NewStreamHandler(repo, streamMedia(t, repo, store, nil, locks), nil, testClientLogger(), WithWaveformGenerator(&fakeWaveformGenerator{}))
	done := make(chan struct{})
	var status int
	var buildErr error
	go func() {
		defer close(done)
		_, status, buildErr = h.audioWaveform(t.Context(), v.ID)
	}()
	var once sync.Once
	release := func() { once.Do(func() { close(barrier.release) }) }
	t.Cleanup(func() { release(); <-done })
	select {
	case <-barrier.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("waveform did not reach publication")
	}
	// Even after the last freshness check, deletion must not purge while Save
	// is still able to recreate the artifact. A waiting deletion is cancelable.
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := deletions.DeleteRecording(ctx, v, repository.DeletionKindManual); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("purge crossed an in-flight waveform publication: %v", err)
	}
	release()
	select {
	case <-done:
		if status != http.StatusOK || buildErr != nil {
			t.Errorf("waveform status=%d err=%v", status, buildErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waveform publication did not finish")
	}
	key, err := repo.GetVideoWaveformKey(t.Context(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := deletions.DeleteRecording(t.Context(), v, repository.DeletionKindManual); err != nil {
		t.Fatal(err)
	}
	if exists, err := store.Exists(t.Context(), key); err != nil || exists {
		t.Fatalf("purge left a waveform: exists=%v err=%v", exists, err)
	}
	assertNoWaveformPublished(t, repo, v.ID)
}

func assertNoWaveformPublished(t *testing.T, repo repository.Repository, videoID int64) {
	t.Helper()
	if key, err := repo.GetVideoWaveformKey(t.Context(), videoID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unexpected waveform reference: %q %v", key, err)
	}
	rows, err := repo.ListRecordingPublications(t.Context(), videoID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if strings.HasSuffix(row.Key, "-waveform.json") {
			t.Fatalf("unexpected waveform publication: %+v", row)
		}
	}
}
