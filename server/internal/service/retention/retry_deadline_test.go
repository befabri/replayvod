package retention

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/testutil/mediatest"
)

type retriedRetentionCandidate struct {
	repository.Repository
	afterDiscovery func() error
}

func (r *retriedRetentionCandidate) ListRetentionCandidates(ctx context.Context, now time.Time, page repository.BatchPage) ([]repository.RetentionVideo, error) {
	rows, err := r.Repository.ListRetentionCandidates(ctx, now, page)
	if err == nil && len(rows) > 0 && r.afterDiscovery != nil {
		hook := r.afterDiscovery
		r.afterDiscovery = nil
		err = hook()
	}
	return rows, err
}

func TestRetentionSweepKeepsRetryCompletedAfterDiscovery(t *testing.T) {
	ctx := t.Context()
	db := testdb.NewSQLiteDB(t)
	repo := &retriedRetentionCandidate{Repository: sqliteadapter.New(db)}
	raw := newLocalStore(t)
	seedChannelUser(t, ctx, repo, "owner", "channel")
	twitchID := "retrying-vod"
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "previous-attempt", Filename: "retrying-recording", DisplayName: "Archive",
		BroadcasterID: "channel", Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		Source: repository.VideoSourceVOD, TwitchVideoID: &twitchID, RetentionWindowHours: ptrInt64(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkVideoFailed(ctx, v.ID, "temporary failure", repository.CompletionKindPartial, false); err != nil {
		t.Fatal(err)
	}
	seedSinglePart(t, repo, v)
	parts, err := repo.ListVideoParts(ctx, v.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("parts: %+v, %v", parts, err)
	}
	key := storagekeys.Video(parts[0].Filename)
	if err := raw.Save(ctx, key, strings.NewReader("saved retry media")); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, "UPDATE videos SET downloaded_at=$1 WHERE id=$2", now.Add(-2*time.Hour).Format(time.RFC3339Nano), v.ID); err != nil {
		t.Fatal(err)
	}
	repo.afterDiscovery = func() error {
		if err := repo.WithTx(ctx, func(tx repository.Repository) error {
			if _, err := tx.CreateJob(ctx, &repository.JobInput{ID: "new-attempt", VideoID: v.ID, BroadcasterID: "channel", Attempt: 2}); err != nil {
				return err
			}
			return tx.RequeueArchiveVideo(ctx, v.ID, "new-attempt", false)
		}); err != nil {
			return err
		}
		return repo.MarkVideoDone(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false)
	}
	svc := New(repo, mediatest.New(t, repo, raw, nil, nil), discardLog())
	deleted, err := svc.Sweep(ctx, now)
	if err != nil || deleted != 0 {
		t.Fatalf("fresh attempt inherited expired deadline: deleted=%d, err=%v", deleted, err)
	}
	fresh, err := repo.GetVideo(ctx, v.ID)
	if err != nil || fresh.JobID != "new-attempt" || fresh.DeletedAt != nil || fresh.Status != repository.VideoStatusDone {
		t.Fatalf("new attempt was removed: %+v, %v", fresh, err)
	}
	if exists, err := raw.Exists(ctx, key); err != nil || !exists {
		t.Fatalf("new attempt lost its media: %v, %v", exists, err)
	}
	deleted, err = svc.Sweep(ctx, now.Add(2*time.Hour))
	if err != nil || deleted != 1 {
		t.Fatalf("new attempt's own deadline was not enforced: deleted=%d, err=%v", deleted, err)
	}
}
