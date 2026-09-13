package mediastore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/recordinglock"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
)

type testGate func(context.Context) error

func (g testGate) Verify(ctx context.Context) error { return g(ctx) }
func mediaFixture(t *testing.T) (repository.Repository, *storage.LocalStorage, *repository.Video) {
	t.Helper()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	raw, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	_, err = repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "channel", BroadcasterLogin: "channel", BroadcasterName: "Channel"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{JobID: "job", Filename: "recording", BroadcasterID: "channel", DisplayName: "Channel", Quality: repository.QualityHigh, Status: repository.VideoStatusDone})
	if err != nil {
		t.Fatal(err)
	}
	return repo, raw, v
}

type failedDelete struct {
	storage.Storage
	key string
}

func (s failedDelete) Delete(ctx context.Context, key string) error {
	if key == s.key {
		return errors.New("first object unavailable")
	}
	return s.Storage.Delete(ctx, key)
}

func TestPublicationCleanupAdvancesBeyondFailedFirstPage(t *testing.T) {
	repo, raw, v := mediaFixture(t)
	ctx := t.Context()
	firstKey := "videos/generation-000"
	for i := range 102 {
		key := fmt.Sprintf("videos/generation-%03d", i)
		if _, err := repo.BeginMediaPublication(ctx, repository.MediaPublication{Key: key, VideoID: v.ID, Digest: "digest"}); err != nil {
			t.Fatal(err)
		}
		if err := repo.ConfirmMediaPublication(ctx, key, "digest"); err != nil {
			t.Fatal(err)
		}
		if err := raw.Save(ctx, key, strings.NewReader("media")); err != nil {
			t.Fatal(err)
		}
	}
	owned, err := managed(t, repo, failedDelete{raw, firstKey}).Lock(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer owned.Close()
	if err := owned.PurgePublications(ctx); err == nil {
		t.Fatal("failed item was not reported")
	}
	rows, err := repo.ListRecordingPublications(ctx, v.ID, "", 200)
	if err != nil || len(rows) != 1 || rows[0].Key != firstKey || !rows[0].DeleteRequested {
		t.Fatalf("cleanup did not advance through all pages: %+v %v", rows, err)
	}
	if exists, err := raw.Exists(ctx, "videos/generation-101"); err != nil || exists {
		t.Fatalf("last page survived failed first item: %v %v", exists, err)
	}
}
func managed(t *testing.T, repo repository.Repository, raw storage.Storage) *Store {
	return New(repo, raw, testGate(func(context.Context) error { return nil }), &recordinglock.Locks{}, t.TempDir())
}

type lateUpload struct {
	storage.Storage
	key, body string
	calls     int
}

func (s *lateUpload) Save(_ context.Context, key string, r io.Reader) error {
	s.calls++
	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.key = key
	s.body = string(body)
	return context.DeadlineExceeded
}
func TestLateUploadRemainsDiscoverableAfterAbsentPurgeAndRestart(t *testing.T) {
	repo, raw, v := mediaFixture(t)
	remote := &lateUpload{Storage: raw}
	store := managed(t, repo, remote)
	ctx := t.Context()
	r, err := store.Lock(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx, "videos/output.mp4", strings.NewReader("captured bytes")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := r.PurgePublications(ctx); err != nil {
		t.Fatal(err)
	}
	r.Close()
	pending, err := repo.GetMediaPublication(ctx, "videos/output.mp4")
	if err != nil || !pending.Unresolved || !pending.DeleteRequested {
		t.Fatalf("lost cleanup intent: %+v %v", pending, err)
	}
	if err := repo.FinalizeDelete(ctx, v.ID, repository.DeletionKindManual); err != nil {
		t.Fatal(err)
	}
	if err := raw.Save(ctx, remote.key, strings.NewReader(remote.body)); err != nil {
		t.Fatal(err)
	}
	if err := managed(t, repo, remote).Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if exists, err := raw.Exists(ctx, remote.key); err != nil || exists {
		t.Fatalf("late object survived: %v %v", exists, err)
	}
	if _, err := repo.GetMediaPublication(ctx, remote.key); err != nil {
		t.Fatalf("uncertain operation retired based on absence: %v", err)
	}
}
func TestPublicationCannotOverwriteAnotherOutputOrPublishAfterDeletion(t *testing.T) {
	repo, raw, v := mediaFixture(t)
	store := managed(t, repo, raw)
	ctx := t.Context()
	r, err := store.Lock(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Save(ctx, "videos/output.mp4", strings.NewReader("first")); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx, "videos/output.mp4", strings.NewReader("different")); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatal(err)
	}
	if err := repo.FinalizeDelete(ctx, v.ID, repository.DeletionKindManual); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx, "videos/new.mp4", strings.NewReader("later")); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatal(err)
	}
	if exists, err := raw.Exists(ctx, "videos/new.mp4"); err != nil || exists {
		t.Fatalf("deleted recording published: %v %v", exists, err)
	}
}

type journalFailure struct{ repository.Repository }

func (r journalFailure) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error { return fn(journalFailure{tx}) })
}
func (journalFailure) BeginMediaPublication(context.Context, repository.MediaPublication) (*repository.MediaPublication, error) {
	return nil, errors.New("database unavailable")
}
func TestPublicationJournalMustCommitBeforeUpload(t *testing.T) {
	repo, raw, v := mediaFixture(t)
	remote := &lateUpload{Storage: raw}
	store := managed(t, journalFailure{repo}, remote)
	r, err := store.Lock(t.Context(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Save(t.Context(), "videos/output.mp4", strings.NewReader("first")); err == nil {
		t.Fatal("journal failure accepted")
	}
	if remote.calls != 0 {
		t.Fatal("upload preceded durable intent")
	}
}
func TestScratchReservationsUseWorkspaceVolumeAndIncludeOtherWriters(t *testing.T) {
	scratch := NewScratch(t.TempDir())
	var checked string
	scratch.stat = func(root string) (int64, int64, error) { checked = root; return 1000, 600, nil }
	first, err := scratch.New("first", 400)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close(true)
	if _, err := scratch.New("second", 200); !errors.Is(err, storage.ErrFull) {
		t.Fatalf("overcommitted scratch: %v", err)
	}
	if checked != scratch.root {
		t.Fatalf("checked wrong volume: %q", checked)
	}
	if err := first.Close(true); err != nil {
		t.Fatal(err)
	}
	second, err := scratch.New("second", 200)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(true)
	scratch.stat = func(string) (int64, int64, error) { return 1000, 40, nil }
	if err := second.Reserve(200); !errors.Is(err, storage.ErrFull) {
		t.Fatalf("external disk growth ignored: %v", err)
	}
}

func TestScratchChunkWritePreservesReservationAndCancelsBeforeDiskFull(t *testing.T) {
	scratch := NewScratch(t.TempDir())
	scratch.stat = func(string) (int64, int64, error) { return 1000, 600, nil }
	capture, err := scratch.New("capture", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close(true)
	copying, err := scratch.New("copy", 400)
	if err != nil {
		t.Fatal(err)
	}
	defer copying.Close(true)
	ctx, join := capture.Monitor(t.Context())
	defer join()
	file, err := os.Create(filepath.Join(capture.Dir, "segment.part"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := capture.WriteFile(ctx, file, make([]byte, 151)); !errors.Is(err, storage.ErrFull) {
		t.Fatalf("other writer's reservation consumed: %v", err)
	}
	if !errors.Is(context.Cause(ctx), storage.ErrFull) {
		t.Fatalf("capture did not stop with recoverable cause: %v", context.Cause(ctx))
	}
	info, err := file.Stat()
	if err != nil || info.Size() != 0 {
		t.Fatalf("over-budget bytes reached disk: %+v %v", info, err)
	}
	if _, err := scratch.Open(t.TempDir(), 0); err == nil {
		t.Fatal("workspace on unaccounted volume accepted")
	}
}
