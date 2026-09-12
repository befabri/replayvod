package retention

import (
	"context"
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
)

type replacingStorage struct {
	storage.Storage
	replacement storage.Storage
	afterDelete func()
	deletes     int
}

func (s *replacingStorage) Delete(ctx context.Context, path string) error {
	s.deletes++
	if err := s.Storage.Delete(ctx, path); err != nil {
		return err
	}
	s.Storage = s.replacement
	s.afterDelete()
	return nil
}

func TestDeleteRecordingStopsBeforeDeletingReplacementStorage(t *testing.T) {
	ctx := t.Context()
	repo, trusted, foreign := newTestRepo(t), newLocalStore(t), newLocalStore(t)
	v := seedRecordingWithObjects(t, ctx, repo, trusted)
	seedRecordingWithObjects(t, ctx, newTestRepo(t), foreign)
	unavailable := false
	store := &replacingStorage{Storage: trusted, replacement: foreign, afterDelete: func() { unavailable = true }}
	svc := New(repo, store, storageGateFunc(func(context.Context) error {
		if unavailable {
			return storage.ErrUnattached
		}
		return nil
	}), discardLog())
	if err := svc.DeleteRecording(ctx, v, repository.DeletionKindRetention); !errors.Is(err, storage.ErrUnattached) {
		t.Fatalf("storage loss during purge: %v", err)
	}
	if store.deletes != 1 {
		t.Fatalf("purge continued onto replacement storage: deletes=%d", store.deletes)
	}
	assertObjectsExist(t, ctx, foreign)
	row, err := repo.GetVideo(ctx, v.ID)
	if err != nil || row.DeletedAt != nil {
		t.Fatalf("partial purge discarded its retry record: %+v %v", row, err)
	}
	// Reattachment retries the original deterministic keys, including the
	// already-deleted first part, and converges without touching the foreign disk.
	svc.store = trusted
	unavailable = false
	if err := svc.DeleteRecording(ctx, v, repository.DeletionKindRetention); err != nil {
		t.Fatal(err)
	}
	assertObjectsGone(t, ctx, trusted)
	assertObjectsExist(t, ctx, foreign)
}
