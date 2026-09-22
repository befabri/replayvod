package storagescan

import (
	"context"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

type invalidCursorRepo struct {
	repository.Repository
	settings repository.ServerSettings
	listed   bool
}

func (r *invalidCursorRepo) GetServerSettings(context.Context) (*repository.ServerSettings, error) {
	return &r.settings, nil
}

func (r *invalidCursorRepo) ListVideosForStorageScan(context.Context, repository.BatchPage) ([]repository.StorageScanVideo, error) {
	r.listed = true
	return nil, nil
}

func (r *invalidCursorRepo) ListMissingTombstones(context.Context, repository.BatchPage) ([]repository.StorageScanVideo, error) {
	r.listed = true
	return nil, nil
}

func TestSweepRejectsInvalidPersistedCursorsBeforeListing(t *testing.T) {
	negative := int64(-1)
	for _, tc := range []struct {
		name     string
		settings repository.ServerSettings
	}{
		{"scan", repository.ServerSettings{StorageScanCursor: negative}},
		{"restore", repository.ServerSettings{StorageRestoreCursor: &negative}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			repo := &invalidCursorRepo{Repository: f.repo, settings: tc.settings}
			f.svc.repo = repo
			report, err := f.svc.Sweep(f.ctx)
			if err == nil || report.Complete || repo.listed {
				t.Fatalf("invalid cursor: report=%+v err=%v listed=%v", report, err, repo.listed)
			}
		})
	}
}
