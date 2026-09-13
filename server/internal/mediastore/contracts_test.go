package mediastore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/recordinglock"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
)

type lostUploadResponse struct {
	storage.Storage
	lost bool
}

func (s *lostUploadResponse) Save(ctx context.Context, key string, r io.Reader) error {
	if err := s.Storage.Save(ctx, key, r); err != nil {
		return err
	}
	if !s.lost {
		s.lost = true
		return context.DeadlineExceeded
	}
	return nil
}

func TestAmbiguousUploadRetryRemainsUnresolvedAndRejectsDifferentBytes(t *testing.T) {
	repo, raw, v := mediaFixture(t)
	remote := &lostUploadResponse{Storage: raw}
	store := managed(t, repo, remote)
	r, err := store.Lock(t.Context(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	key := "videos/ambiguous.mp4"
	if err := r.Save(t.Context(), key, struct{ io.Reader }{strings.NewReader("media")}); err == nil {
		t.Fatal("unprepared nonseekable input accepted")
	}
	if _, err := repo.GetMediaPublication(t.Context(), key); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unprepared input created a journal row: %v", err)
	}
	if err := r.Save(t.Context(), key, strings.NewReader("media")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := r.Save(t.Context(), key, strings.NewReader("media")); err != nil {
		t.Fatal(err)
	}
	row, err := repo.GetMediaPublication(t.Context(), key)
	if err != nil || !row.Unresolved {
		t.Fatalf("retry retired ambiguous request: %+v, %v", row, err)
	}
	if err := r.Save(t.Context(), key, strings.NewReader("different")); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("different retry bytes accepted: %v", err)
	}
	if err := r.Delete(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	row, err = repo.GetMediaPublication(t.Context(), key)
	if err != nil || !row.Unresolved || !row.DeleteRequested {
		t.Fatalf("ambiguous delete lost journal: %+v, %v", row, err)
	}
}

func TestAttemptPublicationAndCommitRejectObsoleteExecution(t *testing.T) {
	repo, raw, v := mediaFixture(t)
	ctx := t.Context()
	if err := repo.UpdateVideoStatus(ctx, v.ID, repository.VideoStatusPending); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateJob(ctx, &repository.JobInput{ID: v.JobID, VideoID: v.ID, BroadcasterID: v.BroadcasterID, ResumeState: []byte(`{"stage":"AUTH","current_part_index":1}`)}); err != nil {
		t.Fatal(err)
	}
	claim := repository.AttemptClaim{JobID: v.JobID, VideoID: v.ID, ExecutionID: "first"}
	if err := repository.ClaimAttempt(ctx, repo, claim, ""); err != nil {
		t.Fatal(err)
	}
	store := managed(t, repo, raw)
	r, err := store.ForAttempt(ctx, claim)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Save(ctx, "videos/owned.mp4", strings.NewReader("media")); err != nil {
		t.Fatal(err)
	}
	commits := 0
	write := func(repository.Repository) error { commits++; return nil }
	if err := r.Commit(ctx, write); err != nil {
		t.Fatal(err)
	}
	claim.ExecutionID = "second"
	if err := repository.ClaimAttempt(ctx, repo, claim, "first"); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(ctx, write); !errors.Is(err, repository.ErrStaleExecution) || commits != 1 {
		t.Fatalf("obsolete commit escaped guard: %d, %v", commits, err)
	}
	if err := r.Save(ctx, "videos/stale.mp4", strings.NewReader("media")); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatal(err)
	}
	r.Close()
	if err := r.Commit(ctx, write); !errors.Is(err, repository.ErrStaleExecution) || commits != 1 {
		t.Fatalf("closed ownership committed: %v", err)
	}
}

func TestManagedReadsRespectStorageVerdictAndReadOnlyDeletion(t *testing.T) {
	repo, raw, v := mediaFixture(t)
	ctx := t.Context()
	var verdict error
	store := New(repo, raw, testGate(func(context.Context) error { return verdict }), &recordinglock.Locks{}, t.TempDir())
	r, err := store.Lock(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	key := "videos/owned.mp4"
	if err := r.Save(ctx, key, strings.NewReader("media")); err != nil {
		t.Fatal(err)
	}
	if store.CapacityRoot() != raw.Root || managed(t, repo, struct{ storage.Storage }{raw}).CapacityRoot() != "" {
		t.Fatal("media capacity used wrong backend root")
	}
	for _, want := range []error{nil, storage.ErrReadOnly, storage.ErrFull, storage.ErrUnattached, storage.ErrUnreachable} {
		verdict = want
		if err := store.Ready(); !errors.Is(err, want) {
			t.Fatalf("readiness=%v, want %v", err, want)
		}
		f, openErr := store.Open(ctx, key)
		if f != nil {
			_ = f.Close()
		}
		_, statErr := store.Stat(ctx, key)
		exists, existsErr := store.Exists(ctx, key)
		if storage.CanRead(want) {
			if openErr != nil || statErr != nil || existsErr != nil || !exists {
				t.Fatalf("readable storage refused: %v, %v, %v", openErr, statErr, existsErr)
			}
		} else if !errors.Is(openErr, want) || !errors.Is(statErr, want) || !errors.Is(existsErr, want) {
			t.Fatalf("unreadable storage accepted: %v, %v, %v", openErr, statErr, existsErr)
		}
	}
	verdict = storage.ErrReadOnly
	if err := r.Delete(ctx, key); !errors.Is(err, storage.ErrReadOnly) {
		t.Fatal(err)
	}
	row, err := repo.GetMediaPublication(ctx, key)
	if err != nil || row.DeleteRequested {
		t.Fatalf("read-only delete changed journal: %+v, %v", row, err)
	}
	if exists, err := raw.Exists(ctx, key); err != nil || !exists {
		t.Fatal("read-only delete removed media")
	}
}

func TestManagedStoreRequiresEverySafetyDependency(t *testing.T) {
	repo, raw, _ := mediaFixture(t)
	for _, missing := range []string{"repository", "backend", "gate", "locks", "scratch"} {
		t.Run(missing, func(t *testing.T) {
			var r repository.Repository = repo
			var b storage.Storage = raw
			var g Gate = testGate(func(context.Context) error { return nil })
			locks, scratch := &recordinglock.Locks{}, t.TempDir()
			switch missing {
			case "repository":
				r = nil
			case "backend":
				b = nil
			case "gate":
				g = nil
			case "locks":
				locks = nil
			case "scratch":
				scratch = ""
			}
			defer func() {
				if recover() == nil {
					t.Fatal("missing safety dependency accepted")
				}
			}()
			New(r, b, g, locks, scratch)
		})
	}
}

type countedPublicationPages struct {
	repository.Repository
	reads atomic.Int32
}

func (r *countedPublicationPages) ListMediaPublications(ctx context.Context, after string, limit int) ([]repository.MediaPublication, error) {
	r.reads.Add(1)
	return r.Repository.ListMediaPublications(ctx, after, limit)
}

type blockedCleanup struct {
	storage.Storage
	entered, release chan struct{}
	blocked          atomic.Bool
}

func (s *blockedCleanup) Delete(ctx context.Context, key string) error {
	if strings.HasSuffix(key, "000") && !s.blocked.Swap(true) {
		close(s.entered)
		<-s.release
		return errors.New("first deletion unavailable")
	}
	return s.Storage.Delete(ctx, key)
}

func TestConcurrentReconcileAdvancesAcrossFailedPageAndWraps(t *testing.T) {
	repo, raw, v := mediaFixture(t)
	ctx := t.Context()
	for i := range 102 {
		key := fmt.Sprintf("videos/cleanup-%03d", i)
		if _, err := repo.BeginMediaPublication(ctx, repository.MediaPublication{Key: key, VideoID: v.ID, Digest: "digest"}); err != nil {
			t.Fatal(err)
		}
		if err := repo.ConfirmMediaPublication(ctx, key, "digest"); err != nil {
			t.Fatal(err)
		}
		if err := repo.RequestMediaPublicationDelete(ctx, key); err != nil {
			t.Fatal(err)
		}
	}
	pages := &countedPublicationPages{Repository: repo}
	backend := &blockedCleanup{Storage: raw, entered: make(chan struct{}), release: make(chan struct{})}
	store := managed(t, pages, backend)
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { first <- store.Reconcile(ctx) }()
	<-backend.entered
	go func() { second <- store.Reconcile(ctx) }()
	time.Sleep(20 * time.Millisecond)
	if reads := pages.reads.Load(); reads != 1 {
		t.Errorf("concurrent reconciler read shared cursor: %d pages", reads)
	}
	close(backend.release)
	if err := <-first; err == nil {
		t.Fatal("first failure was not reported")
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	rows, err := repo.ListRecordingPublications(ctx, v.ID, "", 200)
	if err != nil || len(rows) != 1 || rows[0].Key != "videos/cleanup-000" {
		t.Fatalf("failed prefix starved later page: %+v, %v", rows, err)
	}
	if err := store.Reconcile(ctx); err != nil {
		t.Fatal(err)
	} // Wrap cursor.
	if err := store.Reconcile(ctx); err != nil {
		t.Fatal(err)
	} // Retry first row.
	rows, err = repo.ListRecordingPublications(ctx, v.ID, "", 200)
	if err != nil || len(rows) != 0 {
		t.Fatalf("wrapped cursor did not retry failure: %+v, %v", rows, err)
	}
}
