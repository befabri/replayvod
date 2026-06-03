package video

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
)

// activeRunner is a downloadRunner whose only meaningful method is
// ListActiveProgress; the snapshot path touches nothing else on it.
type activeRunner struct{ progress []downloader.Progress }

func (a activeRunner) Start(context.Context, downloader.Params) (string, error) { return "", nil }
func (a activeRunner) Cancel(string)                                            {}
func (a activeRunner) Subscribe(string) <-chan downloader.Progress              { return nil }
func (a activeRunner) ListActiveProgress() []downloader.Progress                { return a.progress }
func (a activeRunner) SubscribeActive(context.Context) <-chan struct{}          { return nil }

// snapshotDownloadRepo records how ListVideosByJobIDs is called so the test can
// assert the snapshot batches the lookup instead of querying once per job.
type snapshotDownloadRepo struct {
	videos    map[string]repository.Video
	callCount int
	gotJobIDs []string
}

func (r *snapshotDownloadRepo) GetChannel(context.Context, string) (*repository.Channel, error) {
	return nil, repository.ErrNotFound
}

func (r *snapshotDownloadRepo) GetVideoByJobID(context.Context, string) (*repository.Video, error) {
	return nil, repository.ErrNotFound
}

func (r *snapshotDownloadRepo) ListVideosByJobIDs(_ context.Context, jobIDs []string) ([]repository.Video, error) {
	r.callCount++
	r.gotJobIDs = append([]string(nil), jobIDs...)
	out := make([]repository.Video, 0, len(jobIDs))
	for _, id := range jobIDs {
		if v, ok := r.videos[id]; ok {
			out = append(out, v)
		}
	}
	return out, nil
}

// snapshotVideoRepo satisfies repository.Repository by embedding it (unused
// methods panic) and stubs the three best-effort enrichment lookups the
// snapshot makes to empty results.
type snapshotVideoRepo struct{ repository.Repository }

func (snapshotVideoRepo) ListChannelsByIDs(context.Context, []string) ([]repository.Channel, error) {
	return nil, nil
}

func (snapshotVideoRepo) ListPrimaryCategoriesForVideos(context.Context, []int64) (map[int64]repository.Category, error) {
	return nil, nil
}

func (snapshotVideoRepo) ListVideoPartsForVideos(context.Context, []int64) ([]repository.VideoPart, error) {
	return nil, nil
}

func newSnapshotHandler(repo *snapshotDownloadRepo, progress []downloader.Progress) *Handler {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Handler{
		video: &Service{repo: snapshotVideoRepo{}, log: log},
		download: &DownloadService{
			repo:       repo,
			downloader: activeRunner{progress: progress},
			log:        log,
		},
		log: log,
	}
}

func TestActiveDownloadsSnapshot_BatchesVideoLookup(t *testing.T) {
	repo := &snapshotDownloadRepo{videos: map[string]repository.Video{
		"job-a": {ID: 1, JobID: "job-a", BroadcasterID: "bc-1", DisplayName: "A", Quality: "1080p60"},
		"job-b": {ID: 2, JobID: "job-b", BroadcasterID: "bc-2", DisplayName: "B", Quality: "720p"},
	}}
	h := newSnapshotHandler(repo, []downloader.Progress{
		{JobID: "job-a", Stage: "segments", PartIndex: 1, SegmentsTotal: -1, Percent: -1},
		{JobID: "job-b", Stage: "remux", PartIndex: 1, SegmentsTotal: -1, Percent: -1},
	})

	rows, err := h.activeDownloadsSnapshot(context.Background())
	if err != nil {
		t.Fatalf("activeDownloadsSnapshot: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	// The O-3 win: one batched query for the whole active set, not one per job.
	if repo.callCount != 1 {
		t.Fatalf("ListVideosByJobIDs called %d times, want 1 (batched)", repo.callCount)
	}
	if len(repo.gotJobIDs) != 2 {
		t.Fatalf("batched %d job ids, want 2", len(repo.gotJobIDs))
	}

	byJob := map[string]ActiveDownloadResponse{}
	for _, row := range rows {
		byJob[row.Video.JobID] = row
	}
	if got := byJob["job-a"]; got.Video.ID != 1 || got.Stage != "segments" {
		t.Errorf("job-a row = id %d stage %q, want 1/segments", got.Video.ID, got.Stage)
	}
	if got := byJob["job-b"]; got.Video.ID != 2 || got.Stage != "remux" {
		t.Errorf("job-b row = id %d stage %q, want 2/remux", got.Video.ID, got.Stage)
	}
}

func TestActiveDownloadsSnapshot_OmitsJobsWithNoVideoRow(t *testing.T) {
	repo := &snapshotDownloadRepo{videos: map[string]repository.Video{
		"job-a": {ID: 1, JobID: "job-a", BroadcasterID: "bc-1", DisplayName: "A", Quality: "1080p60"},
		// job-ghost has no video row (a stale RUNNING orphan); it must be omitted.
	}}
	h := newSnapshotHandler(repo, []downloader.Progress{
		{JobID: "job-a", Stage: "segments", PartIndex: 1, SegmentsTotal: -1, Percent: -1},
		{JobID: "job-ghost", Stage: "segments", PartIndex: 1, SegmentsTotal: -1, Percent: -1},
	})

	rows, err := h.activeDownloadsSnapshot(context.Background())
	if err != nil {
		t.Fatalf("activeDownloadsSnapshot: %v", err)
	}
	if len(rows) != 1 || rows[0].Video.JobID != "job-a" {
		t.Fatalf("rows = %+v, want only job-a", rows)
	}
	if repo.callCount != 1 {
		t.Fatalf("ListVideosByJobIDs called %d times, want 1", repo.callCount)
	}
}
