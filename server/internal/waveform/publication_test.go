package waveform

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/background"
	"github.com/befabri/replayvod/server/internal/mediastore"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/testutil/mediatest"
)

type delayedWaveform struct {
	storage.Storage
	key       string
	body      []byte
	deferNext bool
}

type panicGenerator struct{}

func (panicGenerator) Generate(context.Context, string, float64, int) ([]float32, error) {
	panic("decoder panic")
}

func TestGeneratorPanicReleasesMaterializedScratch(t *testing.T) {
	repo, raw, _ := waveformPublicationFixture(t)
	store := mediatest.New(t, repo, raw, nil, nil)
	if err := raw.Save(t.Context(), "videos/audio.m4a", strings.NewReader("audio")); err != nil {
		t.Fatal(err)
	}
	err := background.Call(t.Context(), func(ctx context.Context) error {
		_, err := Generate(ctx, panicGenerator{}, InputResolver{Storage: store}, Plan{DurationSeconds: 2, Parts: []Part{{Filename: "audio.m4a", DurationSeconds: 2, Points: 64}}})
		return err
	})
	if err == nil {
		t.Fatal("decoder panic was lost")
	}
	files, err := os.ReadDir(store.Scratch().Root())
	if err != nil || len(files) != 0 {
		t.Fatalf("panic leaked scratch inputs or reservations: %v %v", files, err)
	}
}

func (s *delayedWaveform) Save(ctx context.Context, key string, r io.Reader) error {
	if !s.deferNext {
		return s.Storage.Save(ctx, key, r)
	}
	s.deferNext = false
	s.key = key
	var err error
	s.body, err = io.ReadAll(r)
	if err != nil {
		return err
	}
	return context.DeadlineExceeded
}

func waveformPublicationFixture(t *testing.T) (repository.Repository, *storage.LocalStorage, *repository.Video) {
	t.Helper()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	if _, err := repo.UpsertChannel(t.Context(), &repository.Channel{BroadcasterID: "waveforms", BroadcasterLogin: "waveforms", BroadcasterName: "Waveforms"}); err != nil {
		t.Fatal(err)
	}
	v, err := repo.CreateVideo(t.Context(), &repository.VideoInput{JobID: "waveforms", Filename: "waveforms", BroadcasterID: "waveforms", Status: repository.VideoStatusDone, RecordingType: repository.RecordingTypeAudio})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return repo, raw, v
}

func publishWaveform(t *testing.T, store *mediastore.Store, v *repository.Video, resp Response) error {
	t.Helper()
	owned, err := store.Lock(t.Context(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer owned.Close()
	return SaveArtifact(t.Context(), owned, v.Filename, "same-input", resp)
}

func TestMissingWaveformRegeneratesDifferentBytesAtNewKey(t *testing.T) {
	repo, raw, v := waveformPublicationFixture(t)
	store := mediatest.New(t, repo, raw, nil, nil)
	for i, peak := range []float32{0.1, 0.2} {
		if err := publishWaveform(t, store, v, Response{DurationSeconds: 2, Peaks: []float32{peak}}); err != nil {
			t.Fatal(err)
		}
		key, err := repo.GetVideoWaveformKey(t.Context(), v.ID)
		if err != nil {
			t.Fatal(err)
		}
		got, hit, err := LoadRecording(t.Context(), store, v.ID, "same-input")
		if err != nil || !hit || got.Peaks[0] != peak {
			t.Fatalf("generation %d: %+v %v %v", i, got, hit, err)
		}
		if i == 0 {
			if err := raw.Delete(t.Context(), key); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestPublishingWaveformGenerationPrunesPreviousObjectAndJournal(t *testing.T) {
	repo, raw, v := waveformPublicationFixture(t)
	store := mediatest.New(t, repo, raw, nil, nil)
	if err := publishWaveform(t, store, v, Response{DurationSeconds: 2, Peaks: []float32{0.1}}); err != nil {
		t.Fatal(err)
	}
	previous, err := repo.GetVideoWaveformKey(t.Context(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if exists, err := raw.Exists(t.Context(), previous); err != nil || !exists {
		t.Fatalf("first generation not present: %v, %v", exists, err)
	}
	if err := publishWaveform(t, store, v, Response{DurationSeconds: 2, Peaks: []float32{0.9}}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.GetVideoWaveformKey(t.Context(), v.ID)
	if err != nil || current == previous {
		t.Fatalf("generation not replaced: %q, %v", current, err)
	}
	if exists, err := raw.Exists(t.Context(), previous); err != nil || exists {
		t.Fatalf("superseded waveform still stored: %v, %v", exists, err)
	}
	if _, err := repo.GetMediaPublication(t.Context(), previous); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("superseded journal still present: %v", err)
	}
	if _, err := repo.GetMediaPublication(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	got, hit, err := LoadRecording(t.Context(), store, v.ID, "same-input")
	if err != nil || !hit || got.Peaks[0] != 0.9 {
		t.Fatalf("replacement missing: %+v %v %v", got, hit, err)
	}
}

func TestLateWaveformUploadCannotOverwriteCurrentGeneration(t *testing.T) {
	repo, raw, v := waveformPublicationFixture(t)
	remote := &delayedWaveform{Storage: raw, deferNext: true}
	store := mediatest.New(t, repo, remote, nil, nil)
	if err := publishWaveform(t, store, v, Response{DurationSeconds: 2, Peaks: []float32{0.1}}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err := repo.GetVideoWaveformKey(t.Context(), v.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("ambiguous upload published a reference: %v", err)
	}
	if err := publishWaveform(t, store, v, Response{DurationSeconds: 2, Peaks: []float32{0.9}}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.GetVideoWaveformKey(t.Context(), v.ID)
	if err != nil || current == remote.key {
		t.Fatalf("generation reused unresolved upload key: %q %v", current, err)
	}
	if err := raw.Save(t.Context(), remote.key, bytes.NewReader(remote.body)); err != nil {
		t.Fatal(err)
	}
	// Cleanup must rediscover a delayed upload after service recreation.
	store = mediatest.New(t, repo, raw, nil, nil)
	if err := store.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if exists, err := raw.Exists(t.Context(), remote.key); err != nil || exists {
		t.Fatalf("late orphan survived: %v %v", exists, err)
	}
	got, hit, err := LoadRecording(t.Context(), store, v.ID, "same-input")
	if err != nil || !hit || got.Peaks[0] != 0.9 {
		t.Fatalf("late upload replaced current artifact: %+v %v %v", got, hit, err)
	}
	p, err := repo.GetMediaPublication(t.Context(), remote.key)
	if err != nil || !p.Unresolved || !p.DeleteRequested {
		t.Fatalf("late operation was prematurely forgotten: %+v %v", p, err)
	}
}

type uncertainWaveformReference struct {
	repository.Repository
	lose bool
}

func (r *uncertainWaveformReference) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	wrote := false
	err := r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(waveformReferenceTx{Repository: tx, wrote: &wrote})
	})
	if err == nil && wrote && r.lose {
		r.lose = false
		return repository.ErrCommitUncertain
	}
	return err
}

type waveformReferenceTx struct {
	repository.Repository
	wrote *bool
}

func (r waveformReferenceTx) SetVideoWaveformKey(ctx context.Context, id int64, key string) error {
	*r.wrote = true
	return r.Repository.SetVideoWaveformKey(ctx, id, key)
}

func TestWaveformReferenceRecoversLostCommitConfirmation(t *testing.T) {
	repo, raw, v := waveformPublicationFixture(t)
	fault := &uncertainWaveformReference{Repository: repo, lose: true}
	store := mediatest.New(t, fault, raw, nil, nil)
	if err := publishWaveform(t, store, v, Response{DurationSeconds: 2, Peaks: []float32{0.7}}); !errors.Is(err, repository.ErrCommitUncertain) {
		t.Fatalf("lost reference confirmation: %v", err)
	}
	// A lost commit response must not make the committed waveform unavailable on the next read.
	store = mediatest.New(t, repo, raw, nil, nil)
	got, hit, err := LoadRecording(t.Context(), store, v.ID, "same-input")
	if err != nil || !hit || got.Peaks[0] != 0.7 {
		t.Fatalf("confirmed output became unavailable: %+v %v %v", got, hit, err)
	}
	if err := store.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := LoadRecording(t.Context(), store, v.ID, "same-input"); err != nil || !hit {
		t.Fatalf("reconciliation removed committed output: %v %v", hit, err)
	}
}
