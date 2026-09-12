package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/service/archiveposter"
	"github.com/befabri/replayvod/server/internal/service/storagehealth"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// vodEdge is the smallest Twitch stand-in the queue tests need: a GQL
// endpoint that can be held open (so a job stays in stage 1 and counts as
// active) and a usher that answers 404, which the playback resolver treats
// as final, so a released job fails fast instead of retrying with backoff.
func testPosterStore(t *testing.T, repo repository.Repository, store storage.Storage, log *slog.Logger) *archiveposter.Store {
	t.Helper()
	monitor := storagehealth.New(repo, store, nil, log, "local", "test")
	if _, err := monitor.Attach(t.Context()); err != nil {
		t.Fatal(err)
	}
	return archiveposter.NewStore(repo, store, monitor, &http.Client{Timeout: time.Second}, log)
}

type vodEdge struct {
	srv *httptest.Server

	mu       sync.Mutex
	block    chan struct{}
	gqlVars  []map[string]any
	usherHit int
}

func newVODEdge(t *testing.T) *vodEdge {
	t.Helper()
	e := &vodEdge{}
	mux := http.NewServeMux()
	mux.HandleFunc("/gql", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		e.mu.Lock()
		e.gqlVars = append(e.gqlVars, body.Variables)
		block := e.block
		e.mu.Unlock()
		if block != nil {
			select {
			case <-block:
			case <-r.Context().Done():
				return
			}
		}
		_, _ = io.WriteString(w, `{"data":{"streamPlaybackAccessToken":{"value":"live","signature":"sig"},"videoPlaybackAccessToken":{"value":"vod","signature":"vodsig"}}}`)
	})
	mux.HandleFunc("/poster.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("\xff\xd8\xff\xe0 fake jpeg poster"))
	})
	mux.HandleFunc("/processing.png", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("not a jpeg"))
	})
	mux.HandleFunc("/vod/", func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		e.usherHit++
		e.mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `[{"error_code":"vod_not_found","error":"gone"}]`)
	})
	e.srv = httptest.NewServer(mux)
	t.Cleanup(e.srv.Close)
	return e
}

func (e *vodEdge) hold() (release func()) {
	ch := make(chan struct{})
	e.mu.Lock()
	e.block = ch
	e.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			e.mu.Lock()
			e.block = nil
			e.mu.Unlock()
			close(ch)
		})
	}
}

func (e *vodEdge) lastGQL() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.gqlVars) == 0 {
		return nil
	}
	return e.gqlVars[len(e.gqlVars)-1]
}

type archiveFixture struct {
	svc  *Service
	repo repository.Repository
	edge *vodEdge
}

func newArchiveFixture(t *testing.T, liveCap, archiveCap int) *archiveFixture {
	t.Helper()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	cfg := &config.Config{
		Env: config.Environment{ScratchDir: t.TempDir()},
		App: config.AppConfig{Download: config.DownloadConfig{
			MaxConcurrent:        liveCap,
			ArchiveMaxConcurrent: archiveCap,
			SegmentConcurrency:   2,
			AuthRefreshAttempts:  1,
		}},
		ServerMode: config.ServerModeConfig{Mode: config.ServerModeOff},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(cfg, repo, store, nil, nil, nil, log)
	svc.SetPosterStore(testPosterStore(t, repo, store, log))
	edge := newVODEdge(t)
	svc.twitch = twitch.New(twitch.Config{
		HTTPClient:   &http.Client{Timeout: 10 * time.Second},
		ClientID:     "test",
		UserAgent:    "test",
		DeviceID:     "test-device",
		GQLURL:       edge.srv.URL + "/gql",
		IntegrityURL: edge.srv.URL + "/integrity",
		UsherBaseURL: edge.srv.URL,
	}, log)
	t.Cleanup(svc.Shutdown)
	for _, id := range []string{"bc-1", "bc-2"} {
		if _, err := repo.UpsertChannel(context.Background(), &repository.Channel{
			BroadcasterID: id, BroadcasterLogin: id, BroadcasterName: id,
		}); err != nil {
			t.Fatalf("seed channel %s: %v", id, err)
		}
	}
	return &archiveFixture{svc: svc, repo: repo, edge: edge}
}

func vodParams(broadcaster, vodID string) Params {
	aired := time.Date(2026, 8, 1, 18, 0, 0, 0, time.UTC)
	return Params{
		BroadcasterID:    broadcaster,
		BroadcasterLogin: broadcaster,
		DisplayName:      broadcaster,
		Title:            "vod " + vodID,
		Quality:          repository.QualityHigh,
		RecordingType:    repository.RecordingTypeVideo,
		VODID:            vodID,
		BroadcastAt:      &aired,
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (f *archiveFixture) video(t *testing.T, jobID string) *repository.Video {
	t.Helper()
	v, err := f.repo.GetVideoByJobID(context.Background(), jobID)
	if err != nil {
		t.Fatalf("video for job %s: %v", jobID, err)
	}
	return v
}

func (f *archiveFixture) status(t *testing.T, jobID string) string {
	t.Helper()
	return f.video(t, jobID).Status
}

func (f *archiveFixture) activeJobs() int {
	f.svc.mu.Lock()
	defer f.svc.mu.Unlock()
	return len(f.svc.active)
}

func TestEnqueueVOD_RowShapeAndImmediateStart(t *testing.T) {
	f := newArchiveFixture(t, 2, 1)
	release := f.edge.hold()
	defer release()

	jobID, err := enqueueAndPump(f.svc, context.Background(), vodParams("bc-1", "1001"))
	if err != nil {
		t.Fatalf("EnqueueVOD: %v", err)
	}
	waitUntil(t, "job to go active", func() bool { return f.activeJobs() == 1 })

	v := f.video(t, jobID)
	if v.Source != repository.VideoSourceVOD || v.TwitchVideoID == nil || *v.TwitchVideoID != "1001" {
		t.Fatalf("row source=%q vod=%v, want vod/1001", v.Source, v.TwitchVideoID)
	}
	if v.BroadcastAt == nil || v.BroadcastAt.Year() != 2026 {
		t.Fatalf("broadcast_at = %v, want the VOD air date", v.BroadcastAt)
	}
	if v.Status != repository.VideoStatusRunning {
		t.Fatalf("status = %q, want RUNNING once a slot was free", v.Status)
	}
	if v.Title != "vod 1001" {
		t.Fatalf("title = %q", v.Title)
	}
	job, err := f.repo.GetJob(context.Background(), jobID)
	if err != nil || job.Status != repository.JobStatusRunning {
		t.Fatalf("job = %v, %v; want RUNNING", job, err)
	}
	waitUntil(t, "VOD token request", func() bool { return f.edge.lastGQL() != nil })
	if vars := f.edge.lastGQL(); vars["isVod"] != true || vars["vodID"] != "1001" {
		t.Fatalf("gql variables = %v, want isVod=true vodID=1001", vars)
	}
	if got := f.svc.ListActiveProgress(); len(got) != 1 || got[0].JobID != jobID {
		t.Fatalf("active progress = %+v, want the archive job", got)
	}
}

func TestEnqueueVOD_QueueRespectsCapAndOrder(t *testing.T) {
	f := newArchiveFixture(t, 2, 1)
	release := f.edge.hold()
	defer release()
	ctx := context.Background()

	first, err := enqueueAndPump(f.svc, ctx, vodParams("bc-1", "1"))
	if err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	second, err := enqueueAndPump(f.svc, ctx, vodParams("bc-2", "2"))
	if err != nil {
		t.Fatalf("enqueue second: %v", err)
	}
	third, err := enqueueAndPump(f.svc, ctx, vodParams("bc-1", "3"))
	if err != nil {
		t.Fatalf("enqueue third: %v", err)
	}
	waitUntil(t, "first job active", func() bool { return f.activeJobs() == 1 })
	if f.status(t, first) != repository.VideoStatusRunning || f.status(t, second) != repository.VideoStatusPending || f.status(t, third) != repository.VideoStatusPending {
		t.Fatalf("statuses = %s/%s/%s, want RUNNING/PENDING/PENDING", f.status(t, first), f.status(t, second), f.status(t, third))
	}
	queue, err := f.repo.ListArchiveQueue(ctx)
	if err != nil || len(queue) != 3 {
		t.Fatalf("queue = %d rows, %v; want 3", len(queue), err)
	}

	// Releasing GQL lets the first job hit the 404 usher and fail; the pump
	// must then start the second, and after it fails, the third.
	release()
	waitUntil(t, "first job to fail", func() bool { return f.status(t, first) == repository.VideoStatusFailed })
	waitUntil(t, "second job to start", func() bool {
		s := f.status(t, second)
		return s == repository.VideoStatusRunning || s == repository.VideoStatusFailed
	})
	waitUntil(t, "all jobs terminal", func() bool {
		return f.status(t, second) == repository.VideoStatusFailed && f.status(t, third) == repository.VideoStatusFailed
	})
	v := f.video(t, first)
	if v.CompletionKind != repository.CompletionKindComplete || v.Truncated {
		t.Fatalf("failed archive kind=%q truncated=%v, want complete/false (nothing captured)", v.CompletionKind, v.Truncated)
	}
	if v.Error == nil || *v.Error == "" {
		t.Fatalf("failed archive has no error text")
	}
	if queue, _ := f.repo.ListArchiveQueue(ctx); len(queue) != 0 {
		t.Fatalf("queue still has %d rows after all failed", len(queue))
	}
	waitUntil(t, "no active jobs", func() bool { return f.activeJobs() == 0 })
}

func TestEnqueueVOD_Rejections(t *testing.T) {
	f := newArchiveFixture(t, 2, 1)
	release := f.edge.hold()
	defer release()
	ctx := context.Background()

	if _, err := enqueueAndPump(f.svc, ctx, Params{BroadcasterID: "bc-1", BroadcasterLogin: "bc-1"}); !errors.Is(err, ErrNotVOD) {
		t.Fatalf("no vod id err = %v, want ErrNotVOD", err)
	}
	if _, err := enqueueAndPump(f.svc, ctx, vodParams("bc-1", "7")); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if _, err := enqueueAndPump(f.svc, ctx, vodParams("bc-1", "7")); !errors.Is(err, repository.ErrDuplicate) {
		t.Fatalf("duplicate open archive err = %v, want ErrDuplicate", err)
	}
	f.svc.shuttingDown.Store(true)
	if _, err := enqueueAndPump(f.svc, ctx, vodParams("bc-1", "8")); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("shutting down err = %v, want ErrShuttingDown", err)
	}
	f.svc.shuttingDown.Store(false)
}

func TestArchive_DoesNotTakeLiveSlotsOrBlockChannel(t *testing.T) {
	f := newArchiveFixture(t, 1, 1)
	release := f.edge.hold()
	defer release()
	ctx := context.Background()

	if _, err := enqueueAndPump(f.svc, ctx, vodParams("bc-1", "55")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	waitUntil(t, "archive active", func() bool { return f.activeJobs() == 1 })

	// Same channel, live: the archive must not read as "already recording",
	// and it must not consume the single live slot.
	liveJob, err := f.svc.Start(ctx, Params{BroadcasterID: "bc-1", BroadcasterLogin: "bc-1", DisplayName: "bc-1", Quality: repository.QualityHigh})
	if err != nil {
		t.Fatalf("live Start next to an archive of the same channel: %v", err)
	}
	waitUntil(t, "both active", func() bool { return f.activeJobs() == 2 })
	if f.video(t, liveJob).Source != repository.VideoSourceLive {
		t.Fatalf("live row source = %q", f.video(t, liveJob).Source)
	}
	// The live cap is still enforced among live jobs.
	if _, err := f.svc.Start(ctx, Params{BroadcasterID: "bc-2", BroadcasterLogin: "bc-2", DisplayName: "bc-2", Quality: repository.QualityHigh}); !errors.Is(err, ErrAtCapacity) {
		t.Fatalf("second live Start err = %v, want ErrAtCapacity", err)
	}
	// And the same live channel twice is still busy.
	if _, err := f.svc.Start(ctx, Params{BroadcasterID: "bc-1", BroadcasterLogin: "bc-1", DisplayName: "bc-1", Quality: repository.QualityHigh}); !errors.Is(err, ErrBusy) {
		t.Fatalf("duplicate live Start err = %v, want ErrBusy", err)
	}
	// A second archive waits for the archive slot, not for the live one.
	second, err := enqueueAndPump(f.svc, ctx, vodParams("bc-2", "56"))
	if err != nil {
		t.Fatalf("second enqueue: %v", err)
	}
	if f.status(t, second) != repository.VideoStatusPending {
		t.Fatalf("second archive status = %q, want PENDING behind the archive cap", f.status(t, second))
	}
}

func TestDequeueArchive(t *testing.T) {
	f := newArchiveFixture(t, 2, 1)
	release := f.edge.hold()
	defer release()
	ctx := context.Background()

	running, err := enqueueAndPump(f.svc, ctx, vodParams("bc-1", "1"))
	if err != nil {
		t.Fatalf("enqueue running: %v", err)
	}
	queued, err := enqueueAndPump(f.svc, ctx, vodParams("bc-1", "2"))
	if err != nil {
		t.Fatalf("enqueue queued: %v", err)
	}
	waitUntil(t, "first active", func() bool { return f.activeJobs() == 1 })
	runningID, queuedID := f.video(t, running).ID, f.video(t, queued).ID

	if err := f.svc.DequeueArchive(ctx, runningID); !errors.Is(err, ErrBusy) {
		t.Fatalf("dequeue running err = %v, want ErrBusy", err)
	}
	if err := f.svc.DequeueArchive(ctx, queuedID); err != nil {
		t.Fatalf("dequeue queued: %v", err)
	}
	if _, err := f.repo.GetVideo(ctx, queuedID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("dequeued video still present: %v", err)
	}
	if _, err := f.repo.GetJob(ctx, queued); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("dequeued job still present: %v", err)
	}
	if err := f.svc.DequeueArchive(ctx, queuedID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("dequeue twice err = %v, want ErrNotFound", err)
	}
	// The VOD can be queued again once its row is gone.
	if _, err := enqueueAndPump(f.svc, ctx, vodParams("bc-1", "2")); err != nil {
		t.Fatalf("re-enqueue after dequeue: %v", err)
	}
}

func TestCancelRunningArchive_StartsNextInQueue(t *testing.T) {
	f := newArchiveFixture(t, 2, 1)
	release := f.edge.hold()
	defer release()
	ctx := context.Background()

	first, err := enqueueAndPump(f.svc, ctx, vodParams("bc-1", "1"))
	if err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	second, err := enqueueAndPump(f.svc, ctx, vodParams("bc-2", "2"))
	if err != nil {
		t.Fatalf("enqueue second: %v", err)
	}
	waitUntil(t, "first active", func() bool { return f.activeJobs() == 1 })

	f.svc.Cancel(first)
	waitUntil(t, "first cancelled", func() bool { return f.status(t, first) == repository.VideoStatusFailed })
	if v := f.video(t, first); v.CompletionKind != repository.CompletionKindCancelled {
		t.Fatalf("cancelled archive completion kind = %q", v.CompletionKind)
	}
	waitUntil(t, "second started", func() bool { return f.status(t, second) == repository.VideoStatusRunning })
	// The cancelled job's aborted GQL call may be the last one recorded, so
	// wait for the second job's own token request.
	waitUntil(t, "second job to reach stage 1", func() bool { return f.edge.lastGQL()["vodID"] == "2" })
}

func TestResume_StartsQueuedArchivesAtBoot(t *testing.T) {
	f := newArchiveFixture(t, 2, 1)
	release := f.edge.hold()
	defer release()
	ctx := context.Background()

	// Rows left behind by a previous process: queued, never started.
	for _, id := range []string{"a", "b"} {
		vodID := id
		v, err := f.repo.CreateVideo(ctx, &repository.VideoInput{
			JobID: "job-" + id, Filename: "f-" + id, DisplayName: "bc-1", Title: "t",
			Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
			BroadcasterID: "bc-1", RecordingType: repository.RecordingTypeVideo,
			Source: repository.VideoSourceVOD, TwitchVideoID: &vodID,
		})
		if err != nil {
			t.Fatalf("seed video %s: %v", id, err)
		}
		if _, err := f.repo.CreateJob(ctx, &repository.JobInput{ID: "job-" + id, VideoID: v.ID, BroadcasterID: "bc-1"}); err != nil {
			t.Fatalf("seed job %s: %v", id, err)
		}
	}
	if err := f.svc.Resume(ctx); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	waitUntil(t, "one archive active", func() bool { return f.activeJobs() == 1 })
	waitUntil(t, "VOD token request", func() bool { return f.edge.lastGQL() != nil })
	if f.status(t, "job-a") != repository.VideoStatusRunning || f.status(t, "job-b") != repository.VideoStatusPending {
		t.Fatalf("after boot: a=%s b=%s, want RUNNING/PENDING", f.status(t, "job-a"), f.status(t, "job-b"))
	}
	if vars := f.edge.lastGQL(); vars == nil || vars["isVod"] != true {
		t.Fatalf("resumed archive did not use the VOD token path: %v", vars)
	}
}

func TestShutdown_LeavesQueuedArchivesPending(t *testing.T) {
	f := newArchiveFixture(t, 2, 1)
	release := f.edge.hold()
	defer release()
	ctx := context.Background()

	first, err := enqueueAndPump(f.svc, ctx, vodParams("bc-1", "1"))
	if err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	second, err := enqueueAndPump(f.svc, ctx, vodParams("bc-2", "2"))
	if err != nil {
		t.Fatalf("enqueue second: %v", err)
	}
	waitUntil(t, "first active", func() bool { return f.activeJobs() == 1 })

	f.svc.Shutdown()
	waitUntil(t, "no active jobs", func() bool { return f.activeJobs() == 0 })
	// The interrupted archive stays RUNNING for the next boot's resume; the
	// queued one is untouched and nothing started during shutdown.
	if f.status(t, first) != repository.VideoStatusRunning {
		t.Fatalf("interrupted archive status = %q, want RUNNING", f.status(t, first))
	}
	if f.status(t, second) != repository.VideoStatusPending {
		t.Fatalf("queued archive status = %q, want PENDING", f.status(t, second))
	}
}

func TestEnqueueVOD_StoresTwitchPoster(t *testing.T) {
	f := newArchiveFixture(t, 2, 1)
	release := f.edge.hold()
	defer release()
	ctx := context.Background()

	withPoster := vodParams("bc-1", "1")
	withPoster.PosterURL = f.edge.srv.URL + "/poster.jpg"
	jobID, err := enqueueAndPump(f.svc, ctx, withPoster)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	// The poster lands asynchronously right after the rows exist, as the first
	// snapshot of the recording, and becomes the row thumbnail.
	waitUntil(t, "poster to be stored", func() bool { return f.video(t, jobID).Thumbnail != nil })
	v := f.video(t, jobID)
	want := storagekeys.Snapshot(v.Filename, 0)
	if *v.Thumbnail != want {
		t.Fatalf("thumbnail = %q, want %q", *v.Thumbnail, want)
	}
	if ok, err := f.svc.storage.Exists(ctx, want); err != nil || !ok {
		t.Fatalf("poster object missing (exists=%v err=%v)", ok, err)
	}

	// Twitch's still-processing placeholder and non-JPEG answers are skipped;
	// the row simply has no poster yet.
	for name, url := range map[string]string{
		"placeholder": f.edge.srv.URL + "/404_processing_640x360.png",
		"wrong type":  f.edge.srv.URL + "/processing.png",
		"missing":     f.edge.srv.URL + "/nope.jpg",
	} {
		p := vodParams("bc-2", "poster-"+name)
		p.PosterURL = url
		id, err := enqueueAndPump(f.svc, ctx, p)
		if err != nil {
			t.Fatalf("enqueue %s: %v", name, err)
		}
		time.Sleep(150 * time.Millisecond)
		if got := f.video(t, id).Thumbnail; got != nil {
			t.Fatalf("%s: thumbnail = %q, want none", name, *got)
		}
	}
}

func enqueueAndPump(s *Service, ctx context.Context, p Params) (string, error) {
	id, err := s.EnqueueVOD(ctx, p)
	if err == nil {
		s.PumpArchiveQueue(ctx)
	}
	return id, err
}

// Inject failures inside the real transaction, so the assertions exercise
// rollback rather than a fake repository's idea of atomicity.
type archiveFaultRepo struct {
	repository.Repository
	failCreateJob    bool
	failVideoRunning bool
	failJobFailed    bool
	afterClaim       func()
}

func (r *archiveFaultRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	err := r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(&archiveFaultRepo{Repository: tx, failCreateJob: r.failCreateJob, failVideoRunning: r.failVideoRunning, failJobFailed: r.failJobFailed})
	})
	if err == nil && r.afterClaim != nil {
		r.afterClaim()
	}
	return err
}
func (r *archiveFaultRepo) CreateJob(ctx context.Context, input *repository.JobInput) (*repository.Job, error) {
	if r.failCreateJob {
		return nil, errors.New("injected job insert failure")
	}
	return r.Repository.CreateJob(ctx, input)
}
func (r *archiveFaultRepo) UpdateVideoStatus(ctx context.Context, id int64, status string) error {
	if r.failVideoRunning && status == repository.VideoStatusRunning {
		return errors.New("injected video status failure")
	}
	return r.Repository.UpdateVideoStatus(ctx, id, status)
}

func (r *archiveFaultRepo) MarkJobFailed(ctx context.Context, id, message string) error {
	if r.failJobFailed {
		return errors.New("injected job failure write error")
	}
	return r.Repository.MarkJobFailed(ctx, id, message)
}

func TestArchiveEnqueueRollsBackVideoWhenJobInsertFails(t *testing.T) {
	f := newArchiveFixture(t, 1, 1)
	f.svc.repo = &archiveFaultRepo{Repository: f.repo, failCreateJob: true}
	if _, err := f.svc.EnqueueVOD(t.Context(), vodParams("bc-1", "1")); err == nil {
		t.Fatal("expected insert failure")
	}
	rows, err := f.repo.ListArchiveQueue(t.Context())
	if err != nil || len(rows) != 0 {
		t.Fatalf("orphan video after failed transaction: %+v, %v", rows, err)
	}
}

func TestArchiveClaimRollsBackBothRows(t *testing.T) {
	f := newArchiveFixture(t, 1, 1)
	id, err := f.svc.EnqueueVOD(t.Context(), vodParams("bc-1", "1"))
	if err != nil {
		t.Fatal(err)
	}
	f.svc.repo = &archiveFaultRepo{Repository: f.repo, failVideoRunning: true}
	f.svc.PumpArchiveQueue(t.Context())
	job, err := f.repo.GetJob(t.Context(), id)
	if err != nil || job.Status != "PENDING" || f.status(t, id) != "PENDING" {
		t.Fatalf("claim did not roll back: %+v, %v", job, err)
	}
	if len(f.svc.active) != 0 {
		t.Fatal("started a job whose claim failed")
	}
}

func TestArchiveShutdownBetweenClaimAndStartPreservesResume(t *testing.T) {
	f := newArchiveFixture(t, 1, 1)
	id, err := f.svc.EnqueueVOD(t.Context(), vodParams("bc-1", "1"))
	if err != nil {
		t.Fatal(err)
	}
	f.svc.repo = &archiveFaultRepo{Repository: f.repo, afterClaim: func() { f.svc.shuttingDown.Store(true) }}
	f.svc.PumpArchiveQueue(t.Context())
	job, err := f.repo.GetJob(t.Context(), id)
	if err != nil || job.Status != "RUNNING" || f.status(t, id) != "RUNNING" {
		t.Fatalf("shutdown failed a resumable archive: %+v, %v", job, err)
	}
	if err := f.svc.resumeRunning(t.Context()); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("resume during shutdown: %v", err)
	}
	if f.status(t, id) != "RUNNING" {
		t.Fatal("resume during shutdown failed the video")
	}
}

func TestQueuedArchiveOwnsNoPosterObjects(t *testing.T) {
	f := newArchiveFixture(t, 1, 1)
	p := vodParams("bc-1", "1")
	p.PosterURL = f.edge.srv.URL + "/poster.jpg"
	id, err := f.svc.EnqueueVOD(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	v := f.video(t, id)
	job, err := f.repo.GetJob(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	state, err := UnmarshalResumeState(job.ResumeState)
	if err != nil || state.PosterURL != p.PosterURL {
		t.Fatalf("poster URL not durable: %+v, %v", state, err)
	}
	if err := f.svc.DequeueArchive(t.Context(), v.ID); err != nil {
		t.Fatal(err)
	}
	if exists, err := f.svc.storage.Exists(t.Context(), storagekeys.Snapshot(v.Filename, 0)); err != nil || exists {
		t.Fatalf("queued archive left a poster: exists=%v err=%v", exists, err)
	}
}

func TestArchivePosterCancelledAndJoinedByShutdown(t *testing.T) {
	f := newArchiveFixture(t, 1, 1)
	entered, exited := make(chan struct{}), make(chan struct{})
	poster := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(exited)
	}))
	defer poster.Close()
	p := vodParams("bc-1", "1")
	p.PosterURL = poster.URL
	if _, err := enqueueAndPump(f.svc, t.Context(), p); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("poster fetch did not start")
	}
	f.svc.Shutdown()
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("poster fetch escaped shutdown")
	}
}
