package retention

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testutil/mediatest"
)

type failedPageStorage struct {
	storage.Storage
	fail bool
}

func (s *failedPageStorage) Delete(ctx context.Context, key string) error {
	if s.fail && strings.Contains(key, "blocked-") {
		return errors.New("object unavailable")
	}
	return s.Storage.Delete(ctx, key)
}

func TestDeletionDiscoveryProgressesBeyondFailedPage(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(fmt.Sprint(manual), func(t *testing.T) {
			ctx := t.Context()
			repo, raw := newTestRepo(t), newLocalStore(t)
			objects := &failedPageStorage{Storage: raw, fail: true}
			svc := New(repo, mediatest.New(t, repo, objects, nil, nil), discardLog())
			seedChannelUser(t, ctx, repo, "page-owner", "page-channel")
			pageSize := 100
			if manual {
				pageSize = manualDeleteBatchSize
			}
			var last *repository.Video
			for i := 0; i <= pageSize; i++ {
				name := fmt.Sprintf("blocked-%03d", i)
				if i == pageSize {
					name = "healthy-tail"
				}
				last = seedDoneVideo(t, ctx, repo, name, name, "page-channel")
				seedSinglePart(t, repo, last)
				if manual {
					if _, err := repo.RequestVideoDelete(ctx, last.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			run := func() (int, error) { return svc.Sweep(ctx, time.Now().Add(48*time.Hour)) }
			if manual {
				run = func() (int, error) { return svc.ProcessManualDeletes(ctx) }
			}
			deleted, err := run()
			if err == nil {
				t.Fatal("failed first page was not reported")
			}
			if manual {
				if deleted != 0 {
					t.Fatal("failed first batch reported deletions")
				}
				deleted, err = run()
				if err != nil {
					t.Fatal(err)
				}
			}
			if deleted != 1 {
				t.Fatalf("healthy row after failed page starved: deleted=%d err=%v", deleted, err)
			}
			fresh, err := repo.GetVideo(ctx, last.ID)
			if err != nil || fresh.DeletedAt == nil {
				t.Fatalf("tail did not finish: %+v %v", fresh, err)
			}
			objects.fail = false
			deleted, err = run()
			if err != nil || deleted != pageSize {
				t.Fatalf("cursor failed to revisit recovered items: deleted=%d err=%v", deleted, err)
			}
		})
	}
}
