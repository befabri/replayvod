package retention

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testutil/mediatest"
)

type countedManualPages struct {
	repository.Repository
	pages atomic.Int32
}

func (r *countedManualPages) ListVideosPendingManualDelete(ctx context.Context, after int64, limit int) ([]repository.Video, error) {
	r.pages.Add(1)
	return r.Repository.ListVideosPendingManualDelete(ctx, after, limit)
}

type heldManualDelete struct {
	storage.Storage
	once             sync.Once
	entered, release chan struct{}
}

func (s *heldManualDelete) Delete(ctx context.Context, key string) error {
	s.once.Do(func() { close(s.entered); <-s.release })
	return s.Storage.Delete(ctx, key)
}

func TestConcurrentManualDeletePassesSerializeDiscoveryAndResetShortBatch(t *testing.T) {
	ctx := t.Context()
	repo := &countedManualPages{Repository: newTestRepo(t)}
	raw := &heldManualDelete{Storage: newLocalStore(t), entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(raw.release) }) })
	svc := New(repo, mediatest.New(t, repo, raw, nil, nil), discardLog())
	seedChannelUser(t, ctx, repo, "owner", "channel")
	v := seedDoneVideo(t, ctx, repo, "queued", "queued", "channel")
	seedSinglePart(t, repo, v)
	if _, err := repo.RequestVideoDelete(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	type result struct {
		count int
		err   error
	}
	done := make(chan result, 2)
	go func() { n, err := svc.ProcessManualDeletes(ctx); done <- result{n, err} }()
	select {
	case <-raw.entered:
	case <-time.After(time.Second):
		t.Fatal("delete not started")
	}
	go func() { n, err := svc.ProcessManualDeletes(ctx); done <- result{n, err} }()
	time.Sleep(20 * time.Millisecond)
	if pages := repo.pages.Load(); pages != 1 {
		t.Fatalf("concurrent pass read shared cursor before ownership: %d", pages)
	}
	release.Do(func() { close(raw.release) })
	count := 0
	for range 2 {
		result := <-done
		if result.err != nil {
			t.Fatal(result.err)
		}
		count += result.count
	}
	if count != 1 || svc.manualAfter != 0 {
		t.Fatalf("short batch state: deleted=%d cursor=%d", count, svc.manualAfter)
	}
}

func TestDeleteRecordingRejectsSnapshotFromBeforeArchiveRetry(t *testing.T) {
	ctx := t.Context()
	repo, raw := newTestRepo(t), newLocalStore(t)
	seedChannelUser(t, ctx, repo, "owner", "channel")
	v := seedDoneVideo(t, ctx, repo, "previous-attempt", "recording", "channel")
	seedSinglePart(t, repo, v)
	parts, err := repo.ListVideoParts(ctx, v.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("parts: %+v, %v", parts, err)
	}
	key := "videos/" + parts[0].Filename
	if err := raw.Save(ctx, key, strings.NewReader("saved media")); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{repository.VideoStatusPending, repository.VideoStatusRunning} {
		t.Run(status, func(t *testing.T) {
			if err := repo.UpdateVideoStatus(ctx, v.ID, status); err != nil {
				t.Fatal(err)
			}
			svc := New(repo, mediatest.New(t, repo, raw, nil, nil), discardLog())
			if err := svc.DeleteRecording(ctx, v, repository.DeletionKindRetention); !errors.Is(err, repository.ErrStaleExecution) {
				t.Fatalf("stale deletion accepted: %v", err)
			}
			if exists, err := raw.Exists(ctx, key); err != nil || !exists {
				t.Fatalf("active attempt lost saved media: %v, %v", exists, err)
			}
			fresh, err := repo.GetVideo(ctx, v.ID)
			if err != nil || fresh.DeletedAt != nil || fresh.Status != status {
				t.Fatalf("active attempt was tombstoned: %+v, %v", fresh, err)
			}
		})
	}
}
