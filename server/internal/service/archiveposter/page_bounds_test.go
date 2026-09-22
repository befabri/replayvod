package archiveposter

import (
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testutil/mediatest"
)

type posterPageRepo struct {
	repository.Repository
	listed bool
}

func (r *posterPageRepo) ListArchivesMissingPoster(context.Context, time.Time, repository.BatchPage) ([]repository.Video, error) {
	r.listed = true
	return nil, nil
}

func TestBackfillValidatesBoundsBeforeListing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		after int64
		limit int
		valid bool
	}{
		{"smallest page", 0, 1, true},
		{"largest page", 42, 1000, true},
		{"negative cursor", -1, 100, false},
		{"zero limit", 0, 0, false},
		{"negative limit", 0, -1, false},
		{"oversized page", 0, 1001, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &posterPageRepo{}
			backend, err := storage.NewLocal(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			log := slog.New(slog.DiscardHandler)
			media := mediatest.New(t, repo, backend, readyFunc(func(context.Context) error { return nil }), nil)
			svc := New(NewStore(repo, media, http.DefaultClient, log), repo, &fakeHelix{}, log)
			svc.pageSize = tc.limit
			svc.setResumePoint(tc.after)
			report, err := svc.Backfill(context.Background())
			if (err == nil) != tc.valid || report.Complete != tc.valid || repo.listed != tc.valid {
				t.Fatalf("bounds (%d, %d): report=%+v err=%v listed=%v", tc.after, tc.limit, report, err, repo.listed)
			}
		})
	}
}
