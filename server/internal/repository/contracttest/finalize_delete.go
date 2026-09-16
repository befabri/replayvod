package contracttest

import (
	"errors"
	"fmt"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testFinalizeDelete(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "bc-1")
	mk := func(jobID string) *repository.Video {
		t.Helper()
		id := seedDonePlaybackVideo(t, ctx, repo, jobID, jobID, "bc-1", 2)
		if err := repo.SetVideoThumbnail(ctx, id, "thumbnails/"+jobID+".jpg"); err != nil {
			t.Fatal(err)
		}
		v, err := repo.GetVideo(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	parts := func(id int64) int64 {
		t.Helper()
		n, err := repo.CountVideoParts(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	reload := func(v *repository.Video) *repository.Video {
		t.Helper()
		got, err := repo.GetVideo(ctx, v.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	kindOf := func(v *repository.Video) string {
		if v.DeletionKind == nil {
			return ""
		}
		return *v.DeletionKind
	}
	live := mk("finalize-live")
	queued := mk("finalize-queued")
	missing := mk("finalize-missing")
	purged := mk("finalize-purged")
	bystander := mk("finalize-bystander")
	if _, err := repo.RequestVideoDelete(ctx, queued.ID); err != nil {
		t.Fatal(err)
	}
	if changed, err := repo.TombstoneMissingVideo(ctx, missing.ID); err != nil || !changed {
		t.Fatalf("missing tombstone = %v, %v", changed, err)
	}
	if err := repo.SoftDeleteVideo(ctx, purged.ID, repository.DeletionKindRetention); err != nil {
		t.Fatal(err)
	}

	abort := errors.New("abort finalize")
	err := repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := tx.FinalizeDelete(ctx, live.ID, repository.DeletionKindRetention); err != nil {
			return err
		}
		if n, err := tx.CountVideoParts(ctx, live.ID); err != nil || n != 0 {
			return fmt.Errorf("parts inside the transaction = %d, %v", n, err)
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatalf("aborted finalize err = %v, want %v", err, abort)
	}
	if got := reload(live); got.DeletedAt != nil || got.Thumbnail == nil || parts(live.ID) != 2 {
		t.Fatalf("rolled-back finalize left a change: %+v, %d parts", got, parts(live.ID))
	}

	if err := repo.FinalizeDelete(ctx, live.ID, repository.DeletionKindRetention); err != nil {
		t.Fatal(err)
	}
	gone := reload(live)
	if gone.DeletedAt == nil || kindOf(gone) != repository.DeletionKindRetention || gone.Thumbnail != nil || gone.DeleteRequestedAt != nil || parts(live.ID) != 0 {
		t.Fatalf("finalized video = %+v, %d parts", gone, parts(live.ID))
	}
	if err := repo.FinalizeDelete(ctx, live.ID, repository.DeletionKindManual); err != nil {
		t.Fatalf("repeated finalize: %v", err)
	}
	if again := reload(live); kindOf(again) != repository.DeletionKindRetention || !again.DeletedAt.Equal(*gone.DeletedAt) {
		t.Fatalf("repeated finalize rewrote the tombstone: %+v", again)
	}

	if err := repo.FinalizeDelete(ctx, queued.ID, repository.DeletionKindRetention); err != nil {
		t.Fatal(err)
	}
	if got := reload(queued); got.DeletedAt == nil || kindOf(got) != repository.DeletionKindManual || got.DeleteRequestedAt != nil || parts(queued.ID) != 0 {
		t.Fatalf("queued manual delete finalized as %+v, %d parts; want the manual kind", got, parts(queued.ID))
	}
	if pending, err := repo.ListVideosPendingManualDelete(ctx, 0, 10); err != nil || len(pending) != 0 {
		t.Fatalf("finalized video still queued: %v, %v", videoJobIDs(pending), err)
	}

	before := reload(missing)
	if before.Thumbnail == nil || parts(missing.ID) != 2 {
		t.Fatalf("missing tombstone lost its media metadata early: %+v, %d parts", before, parts(missing.ID))
	}
	if err := repo.FinalizeDelete(ctx, missing.ID, repository.DeletionKindRetention); err != nil {
		t.Fatal(err)
	}
	if got := reload(missing); kindOf(got) != repository.DeletionKindRetention || got.Thumbnail != nil || got.DeletedAt.Before(*before.DeletedAt) || parts(missing.ID) != 0 {
		t.Fatalf("finalized missing tombstone = %+v, %d parts", got, parts(missing.ID))
	}
	if _, err := repo.GetMissingTombstone(ctx, missing.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("finalized tombstone still restorable: %v", err)
	}

	if err := repo.FinalizeDelete(ctx, purged.ID, repository.DeletionKindManual); err != nil {
		t.Fatal(err)
	}
	if got := reload(purged); kindOf(got) != repository.DeletionKindRetention || parts(purged.ID) != 0 {
		t.Fatalf("finalize of a purged tombstone = %+v, %d parts; want the kind kept and the parts gone", got, parts(purged.ID))
	}
	if err := repo.FinalizeDelete(ctx, bystander.ID+1000, repository.DeletionKindRetention); err != nil {
		t.Fatalf("finalize of an unknown video: %v", err)
	}
	if got := reload(bystander); got.DeletedAt != nil || got.Thumbnail == nil || parts(bystander.ID) != 2 {
		t.Fatalf("bystander changed: %+v, %d parts", got, parts(bystander.ID))
	}
}
