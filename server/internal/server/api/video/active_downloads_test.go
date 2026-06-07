package video

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
)

// signalRunner is an activeRunner with a controllable SubscribeActive channel,
// so a test can drive ActiveDownloadsLive's coalescing loop deterministically.
type signalRunner struct {
	activeRunner
	sig <-chan struct{}
}

func (r signalRunner) SubscribeActive(context.Context) <-chan struct{} { return r.sig }

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
	videos     map[string]repository.Video
	callCount  int
	gotJobIDs  []string
	failOnCall map[int]error
	callCh     chan int
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
	if r.callCh != nil {
		r.callCh <- r.callCount
	}
	if err := r.failOnCall[r.callCount]; err != nil {
		return nil, err
	}
	out := make([]repository.Video, 0, len(jobIDs))
	for _, id := range jobIDs {
		if v, ok := r.videos[id]; ok {
			out = append(out, v)
		}
	}
	return out, nil
}

func waitForSnapshotRepoCall(t *testing.T, calls <-chan int, want int) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case got := <-calls:
			if got == want {
				return
			}
			if got > want {
				t.Fatalf("observed snapshot repo call %d, want %d", got, want)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for snapshot repo call %d", want)
		}
	}
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

func (snapshotVideoRepo) ListVideoUserStatesForVideos(context.Context, string, []int64) ([]repository.VideoUserState, error) {
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

func recvActiveDownloadsSnapshot(t *testing.T, out <-chan []ActiveDownloadResponse) []ActiveDownloadResponse {
	t.Helper()
	select {
	case rows, ok := <-out:
		if !ok {
			t.Fatal("active downloads stream closed before next snapshot")
		}
		return rows
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for active downloads snapshot")
		return nil
	}
}

func activeDownloadsTestContext(parent context.Context) context.Context {
	return middleware.WithUser(parent, &repository.User{ID: "u1"})
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

	rows, err := h.activeDownloadsSnapshot(context.Background(), "u1")
	if err != nil {
		t.Fatalf("activeDownloadsSnapshot: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
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

// The interval is huge so the only snapshots are the initial one and the
// coalesced flush when the source closes.
func TestActiveDownloadsLive_CoalescesBurstOfPokes(t *testing.T) {
	prev := activeDownloadsCoalesceInterval
	activeDownloadsCoalesceInterval = time.Hour
	t.Cleanup(func() { activeDownloadsCoalesceInterval = prev })

	sig := make(chan struct{}, 64)
	repo := &snapshotDownloadRepo{videos: map[string]repository.Video{
		"job-a": {ID: 1, JobID: "job-a", BroadcasterID: "bc-1", DisplayName: "A", Quality: "1080p60"},
	}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := &Handler{
		video: &Service{repo: snapshotVideoRepo{}, log: log},
		download: &DownloadService{
			repo: repo,
			downloader: signalRunner{
				activeRunner: activeRunner{progress: []downloader.Progress{
					{JobID: "job-a", Stage: "segments", PartIndex: 1, SegmentsTotal: -1, Percent: -1},
				}},
				sig: sig,
			},
			log: log,
		},
		log: log,
	}

	ctx, cancel := context.WithCancel(activeDownloadsTestContext(context.Background()))
	defer cancel()
	out, err := h.ActiveDownloadsLive(ctx)
	if err != nil {
		t.Fatalf("ActiveDownloadsLive: %v", err)
	}

	const pokes = 50
	for i := 0; i < pokes; i++ {
		sig <- struct{}{}
	}
	close(sig)

	snapshots := 0
	for range out {
		snapshots++
	}
	if snapshots != 2 {
		t.Fatalf("got %d snapshots for %d pokes, want 2 (initial + one coalesced flush)", snapshots, pokes)
	}
	if repo.callCount != 2 {
		t.Fatalf("DB re-queried %d times for %d pokes, want 2 (coalesced)", repo.callCount, pokes)
	}
}

func TestActiveDownloadsLive_RetriesFailedPendingSnapshot(t *testing.T) {
	prev := activeDownloadsCoalesceInterval
	activeDownloadsCoalesceInterval = 50 * time.Millisecond
	t.Cleanup(func() { activeDownloadsCoalesceInterval = prev })

	sig := make(chan struct{}, 1)
	sig <- struct{}{}
	repo := &snapshotDownloadRepo{
		videos: map[string]repository.Video{
			"job-a": {ID: 1, JobID: "job-a", BroadcasterID: "bc-1", DisplayName: "A", Quality: "1080p60"},
		},
		failOnCall: map[int]error{
			2: errors.New("temporary snapshot failure"),
		},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := &Handler{
		video: &Service{repo: snapshotVideoRepo{}, log: log},
		download: &DownloadService{
			repo: repo,
			downloader: signalRunner{
				activeRunner: activeRunner{progress: []downloader.Progress{
					{JobID: "job-a", Stage: "segments", PartIndex: 1, SegmentsTotal: -1, Percent: -1},
				}},
				sig: sig,
			},
			log: log,
		},
		log: log,
	}

	ctx, cancel := context.WithCancel(activeDownloadsTestContext(context.Background()))
	out, err := h.ActiveDownloadsLive(ctx)
	if err != nil {
		t.Fatalf("ActiveDownloadsLive: %v", err)
	}

	if rows := recvActiveDownloadsSnapshot(t, out); len(rows) != 1 {
		t.Fatalf("initial snapshot rows = %d, want 1", len(rows))
	}
	if rows := recvActiveDownloadsSnapshot(t, out); len(rows) != 1 {
		t.Fatalf("retried snapshot rows = %d, want 1", len(rows))
	}

	cancel()
	for range out {
	}
	if repo.callCount != 3 {
		t.Fatalf("DB re-queried %d times, want 3 (initial + failed flush + retry)", repo.callCount)
	}
}

func TestActiveDownloadsLive_RetriesFailedInitialSnapshot(t *testing.T) {
	prev := activeDownloadsCoalesceInterval
	activeDownloadsCoalesceInterval = 50 * time.Millisecond
	t.Cleanup(func() { activeDownloadsCoalesceInterval = prev })

	sig := make(chan struct{})
	repo := &snapshotDownloadRepo{
		videos: map[string]repository.Video{
			"job-a": {ID: 1, JobID: "job-a", BroadcasterID: "bc-1", DisplayName: "A", Quality: "1080p60"},
		},
		failOnCall: map[int]error{
			1: errors.New("temporary initial snapshot failure"),
		},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := &Handler{
		video: &Service{repo: snapshotVideoRepo{}, log: log},
		download: &DownloadService{
			repo: repo,
			downloader: signalRunner{
				activeRunner: activeRunner{progress: []downloader.Progress{
					{JobID: "job-a", Stage: "segments", PartIndex: 1, SegmentsTotal: -1, Percent: -1},
				}},
				sig: sig,
			},
			log: log,
		},
		log: log,
	}

	ctx, cancel := context.WithCancel(activeDownloadsTestContext(context.Background()))
	out, err := h.ActiveDownloadsLive(ctx)
	if err != nil {
		t.Fatalf("ActiveDownloadsLive: %v", err)
	}

	if rows := recvActiveDownloadsSnapshot(t, out); len(rows) != 1 {
		t.Fatalf("retried initial snapshot rows = %d, want 1", len(rows))
	}

	cancel()
	for range out {
	}
	if repo.callCount != 2 {
		t.Fatalf("DB re-queried %d times, want 2 (failed initial + retry)", repo.callCount)
	}
}

func TestActiveDownloadsLive_TerminalFlushWaitsForContextWhenSnapshotFails(t *testing.T) {
	prev := activeDownloadsCoalesceInterval
	activeDownloadsCoalesceInterval = time.Hour
	t.Cleanup(func() { activeDownloadsCoalesceInterval = prev })

	sig := make(chan struct{}, 1)
	sig <- struct{}{}
	close(sig)
	calls := make(chan int, 4)
	repo := &snapshotDownloadRepo{
		videos: map[string]repository.Video{
			"job-a": {ID: 1, JobID: "job-a", BroadcasterID: "bc-1", DisplayName: "A", Quality: "1080p60"},
		},
		failOnCall: map[int]error{
			2: errors.New("terminal snapshot still failing"),
		},
		callCh: calls,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := &Handler{
		video: &Service{repo: snapshotVideoRepo{}, log: log},
		download: &DownloadService{
			repo: repo,
			downloader: signalRunner{
				activeRunner: activeRunner{progress: []downloader.Progress{
					{JobID: "job-a", Stage: "segments", PartIndex: 1, SegmentsTotal: -1, Percent: -1},
				}},
				sig: sig,
			},
			log: log,
		},
		log: log,
	}

	ctx, cancel := context.WithCancel(activeDownloadsTestContext(context.Background()))
	out, err := h.ActiveDownloadsLive(ctx)
	if err != nil {
		t.Fatalf("ActiveDownloadsLive: %v", err)
	}

	if rows := recvActiveDownloadsSnapshot(t, out); len(rows) != 1 {
		t.Fatalf("initial snapshot rows = %d, want 1", len(rows))
	}
	waitForSnapshotRepoCall(t, calls, 2)

	select {
	case _, ok := <-out:
		if !ok {
			t.Fatal("active downloads stream closed before context cancellation")
		}
		t.Fatal("active downloads stream delivered an unexpected snapshot after failed terminal flush")
	case <-time.After(25 * time.Millisecond):
	}

	cancel()
	select {
	case _, ok := <-out:
		if ok {
			t.Fatal("active downloads stream delivered an unexpected snapshot after context cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("active downloads stream did not close after context cancellation")
	}
	if repo.callCount != 2 {
		t.Fatalf("DB re-queried %d times, want 2 (initial + failed terminal flush)", repo.callCount)
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

	rows, err := h.activeDownloadsSnapshot(context.Background(), "u1")
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
