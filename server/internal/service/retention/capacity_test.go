package retention

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/testutil/mediatest"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/storagehealth"
	"github.com/befabri/replayvod/server/internal/storage"
)

type fullStorage struct {
	*storage.LocalStorage
	cause error
}

func (s fullStorage) ProbeWrite(context.Context) error {
	return &os.PathError{Op: "create", Path: s.Root, Err: s.cause}
}

func TestFullDiskAllowsRetentionAndManualDeletion(t *testing.T) {
	for _, cause := range []error{syscall.ENOSPC, syscall.EDQUOT} {
		for _, kind := range []string{repository.DeletionKindRetention, repository.DeletionKindManual} {
			t.Run(cause.Error()+"/"+string(kind), func(t *testing.T) {
				ctx := t.Context()
				repo, local := newTestRepo(t), newLocalStore(t)
				v := seedRecordingWithObjects(t, ctx, repo, local)
				store := fullStorage{local, cause}
				mon := storagehealth.New(repo, store, nil, discardLog(), "local", local.Root)
				if st, err := mon.Attach(ctx); st.State != storagehealth.StateFull || !errors.Is(err, storage.ErrFull) {
					t.Fatalf("full disk fixture: %+v %v", st, err)
				}
				svc := New(repo, mediatest.New(t, repo, store, mon, nil), discardLog())
				if kind == repository.DeletionKindRetention {
					if n, err := svc.Sweep(ctx, time.Now().Add(2*time.Hour)); err != nil || n != 1 {
						t.Fatalf("retention on full storage: deleted=%d err=%v", n, err)
					}
				} else if err := svc.DeleteRecording(ctx, v, kind); err != nil {
					t.Fatalf("manual deletion on full storage: %v", err)
				}
				assertObjectsGone(t, ctx, local)
				row, err := repo.GetVideo(ctx, v.ID)
				if err != nil || row.DeletedAt == nil || row.DeletionKind == nil || *row.DeletionKind != kind {
					t.Fatalf("cleanup not committed: %+v %v", row, err)
				}
				if err := mon.Ready(); !errors.Is(err, storage.ErrFull) {
					t.Fatalf("cleanup incorrectly enabled recording: %v", err)
				}
			})
		}
	}
}
