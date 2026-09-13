package archiveposter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/testutil/mediatest"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

type readyFunc func(context.Context) error

func (f readyFunc) Verify(ctx context.Context) error { return f(ctx) }

type fakeHelix struct {
	videos map[string]twitch.Video
	calls  int
}

func (f *fakeHelix) GetVideos(_ context.Context, p *twitch.GetVideosParams) ([]twitch.Video, twitch.Pagination, error) {
	f.calls++
	var out []twitch.Video
	missing := false
	for _, id := range p.ID {
		if v, ok := f.videos[id]; ok {
			out = append(out, v)
		} else {
			missing = true
		}
	}
	if missing {
		return nil, twitch.Pagination{}, &twitch.HelixError{Status: http.StatusNotFound, Body: "vod not found"}
	}
	return out, twitch.Pagination{}, nil
}

func seedArchive(t *testing.T, repo repository.Repository, jobID, vodID string) *repository.Video {
	t.Helper()
	id := vodID
	v, err := repo.CreateVideo(context.Background(), &repository.VideoInput{
		JobID: jobID, Filename: jobID, DisplayName: "bc-1", Title: "vod",
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: "bc-1", RecordingType: repository.RecordingTypeVideo,
		Source: repository.VideoSourceVOD, TwitchVideoID: &id,
	})
	if err != nil {
		t.Fatalf("seed archive %s: %v", jobID, err)
	}
	return v
}

func TestBackfill(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "bc-1", BroadcasterLogin: "bc-1", BroadcasterName: "bc-1"}); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/poster-640x360.jpg" {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, "\xff\xd8\xff\xe0 fake jpeg")
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer cdn.Close()

	rendered := seedArchive(t, repo, "job-rendered", "1")
	processing := seedArchive(t, repo, "job-processing", "2")
	gone := seedArchive(t, repo, "job-gone", "3")
	framed := seedArchive(t, repo, "job-framed", "4")

	helix := &fakeHelix{videos: map[string]twitch.Video{
		"1": {ID: "1", ThumbnailURL: cdn.URL + "/poster-%{width}x%{height}.jpg"},
		"2": {ID: "2", ThumbnailURL: "https://vod-secure.twitch.tv/_404/404_processing_%{width}x%{height}.png"},
		"4": {ID: "4", ThumbnailURL: cdn.URL + "/poster-%{width}x%{height}.jpg"},
	}}
	svc := New(NewStore(repo, mediatest.New(t, repo, store, readyFunc(func(context.Context) error { return nil }), nil), cdn.Client(), slog.New(slog.DiscardHandler)), repo, helix, slog.New(slog.DiscardHandler))
	svc.pageSize = 2
	// A frame produced by the pipeline between the listing and the fetch wins.
	racer := frameRacer{Repository: repo, videoID: framed.ID}
	svc.store.repo = racer
	svc.store.storage = mediatest.New(t, racer, store, readyFunc(func(context.Context) error { return nil }), nil)

	report, err := svc.Backfill(ctx)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if report.Checked != 3 || report.Stored != 1 {
		t.Fatalf("report = %+v, want 3 checked, 1 stored", report)
	}
	if helix.calls < 2 {
		t.Fatalf("helix calls = %d, want the batch plus per-id fallbacks for the unknown VOD", helix.calls)
	}

	got, _ := repo.GetVideo(ctx, rendered.ID)
	want := storagekeys.Snapshot(rendered.Filename, 0)
	if got.Thumbnail == nil || *got.Thumbnail != want {
		t.Fatalf("rendered thumbnail = %v, want %q", got.Thumbnail, want)
	}
	if ok, _ := store.Exists(ctx, want); !ok {
		t.Fatalf("poster object %q missing", want)
	}
	for name, v := range map[string]*repository.Video{"processing": processing, "gone": gone} {
		got, _ := repo.GetVideo(ctx, v.ID)
		if got.Thumbnail != nil {
			t.Fatalf("%s: thumbnail = %q, want none", name, *got.Thumbnail)
		}
		if ok, _ := store.Exists(ctx, storagekeys.Snapshot(v.Filename, 0)); ok {
			t.Fatalf("%s: a poster object was stored", name)
		}
	}
	got, _ = repo.GetVideo(ctx, framed.ID)
	if got.Thumbnail == nil || *got.Thumbnail != "thumbnails/frame.jpg" {
		t.Fatalf("framed thumbnail = %v, want the pipeline frame kept", got.Thumbnail)
	}
	if ok, _ := store.Exists(ctx, storagekeys.Snapshot(framed.Filename, 0)); ok {
		t.Fatal("the losing poster object was not cleaned up")
	}

	svc.now = func() time.Time { return time.Now().Add(Window + time.Hour) }
	report, err = svc.Backfill(ctx)
	if err != nil || report.Checked != 0 {
		t.Fatalf("report after window = %+v, %v; want nothing checked", report, err)
	}
}

type frameRacer struct {
	repository.Repository
	videoID int64
}

func (r frameRacer) SetVideoThumbnailIfMissing(ctx context.Context, id int64, thumbnail string) (bool, error) {
	if id == r.videoID {
		if err := r.SetVideoThumbnail(ctx, id, "thumbnails/frame.jpg"); err != nil {
			return false, err
		}
	}
	return r.Repository.SetVideoThumbnailIfMissing(ctx, id, thumbnail)
}

func posterServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("\xff\xd8\xff\xe0jpeg"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestFetch_LosingFetchKeepsTheObjectTheWinnerReferences prevents cleanup of a
// poster retained by an earlier fetch.
func TestFetch_LosingFetchKeepsTheObjectTheWinnerReferences(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "bc-1", BroadcasterLogin: "bc-1", BroadcasterName: "bc-1"}); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := posterServer(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	posters := NewStore(repo, mediatest.New(t, repo, store, readyFunc(func(context.Context) error { return nil }), nil), srv.Client(), log)
	v := seedArchive(t, repo, "job-1", "1001")
	key := storagekeys.Snapshot(v.Filename, 0)

	if !posters.Fetch(ctx, v.ID, v.Filename, srv.URL+"/a.jpg") {
		t.Fatal("first fetch did not store the poster")
	}
	if posters.Fetch(ctx, v.ID, v.Filename, srv.URL+"/a.jpg") {
		t.Fatal("second fetch claimed to store a poster the row already had")
	}
	if ok, err := store.Exists(ctx, key); err != nil || !ok {
		t.Fatalf("the losing fetch deleted the referenced poster: exists=%v, %v", ok, err)
	}
	got, err := repo.GetVideo(ctx, v.ID)
	if err != nil || got.Thumbnail == nil || *got.Thumbnail != key {
		t.Fatalf("thumbnail = %v, %v; want %s", got.Thumbnail, err, key)
	}

	framed := seedArchive(t, repo, "job-2", "1002")
	if err := repo.SetVideoThumbnail(ctx, framed.ID, storagekeys.Thumbnail(framed.Filename+"-part01")); err != nil {
		t.Fatal(err)
	}
	if posters.Fetch(ctx, framed.ID, framed.Filename, srv.URL+"/b.jpg") {
		t.Fatal("fetch replaced a pipeline frame")
	}
	if ok, err := store.Exists(ctx, storagekeys.Snapshot(framed.Filename, 0)); err != nil || ok {
		t.Fatalf("unreferenced poster kept: exists=%v, %v", ok, err)
	}
}

type unreadableVideoRepo struct {
	repository.Repository
}

func (unreadableVideoRepo) GetVideo(context.Context, int64) (*repository.Video, error) {
	return nil, context.DeadlineExceeded
}

// TestFetch_UnreadableRowKeepsTheObject preserves a potentially referenced poster
// when the reference cannot be read after publication.
func TestFetch_UnreadableRowKeepsTheObject(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "bc-1", BroadcasterLogin: "bc-1", BroadcasterName: "bc-1"}); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := posterServer(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	v := seedArchive(t, repo, "job-1", "1001")
	if err := repo.SetVideoThumbnail(ctx, v.ID, storagekeys.Thumbnail(v.Filename+"-part01")); err != nil {
		t.Fatal(err)
	}
	posters := NewStore(unreadableVideoRepo{Repository: repo}, mediatest.New(t, unreadableVideoRepo{Repository: repo}, store, readyFunc(func(context.Context) error { return nil }), nil), srv.Client(), log)
	if posters.Fetch(ctx, v.ID, v.Filename, srv.URL+"/a.jpg") {
		t.Fatal("fetch reported a stored poster")
	}
	if ok, err := store.Exists(ctx, storagekeys.Snapshot(v.Filename, 0)); err != nil || !ok {
		t.Fatalf("object deleted on an unreadable row: exists=%v, %v", ok, err)
	}
}

func posterFixture(t *testing.T) (repository.Repository, *storage.LocalStorage) {
	t.Helper()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	if _, err := repo.UpsertChannel(context.Background(), &repository.Channel{BroadcasterID: "bc-1", BroadcasterLogin: "bc-1", BroadcasterName: "bc-1"}); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return repo, store
}

func TestFetch_OneFetchOwnsTheKeyWhileInFlight(t *testing.T) {
	ctx := context.Background()
	repo, store := posterFixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			close(started)
			<-release
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("\xff\xd8\xff\xe0jpeg"))
	}))
	defer srv.Close()
	posters := NewStore(repo, mediatest.New(t, repo, store, readyFunc(func(context.Context) error { return nil }), nil), srv.Client(), slog.New(slog.DiscardHandler))
	v := seedArchive(t, repo, "job-1", "1001")

	first := make(chan bool, 1)
	go func() { first <- posters.Fetch(ctx, v.ID, v.Filename, srv.URL+"/a.jpg") }()
	<-started
	if posters.Fetch(ctx, v.ID, v.Filename, srv.URL+"/a.jpg") {
		t.Fatal("a second fetch stored a poster while the first was in flight")
	}
	close(release)
	if !<-first {
		t.Fatal("the first fetch did not store the poster")
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("image downloads = %d, want 1", hits)
	}
	key := storagekeys.Snapshot(v.Filename, 0)
	if ok, err := store.Exists(ctx, key); err != nil || !ok {
		t.Fatalf("poster object missing after the race: %v, %v", ok, err)
	}
	got, err := repo.GetVideo(ctx, v.ID)
	if err != nil || got.Thumbnail == nil || *got.Thumbnail != key {
		t.Fatalf("thumbnail = %v, %v; want %s", got.Thumbnail, err, key)
	}
	if posters.Fetch(ctx, v.ID, v.Filename, srv.URL+"/a.jpg") || atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("a fetch after the poster was set downloaded again: hits=%d", hits)
	}
}

func TestFetch_SkipsRowsThatNeedNoPoster(t *testing.T) {
	ctx := context.Background()
	repo, store := posterFixture(t)
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("\xff\xd8\xff\xe0jpeg"))
	}))
	defer srv.Close()
	posters := NewStore(repo, mediatest.New(t, repo, store, readyFunc(func(context.Context) error { return nil }), nil), srv.Client(), slog.New(slog.DiscardHandler))

	framed := seedArchive(t, repo, "job-framed", "1001")
	if err := repo.SetVideoThumbnail(ctx, framed.ID, "thumbnails/frame.jpg"); err != nil {
		t.Fatal(err)
	}
	removed := seedArchive(t, repo, "job-removed", "1002")
	if err := repo.SoftDeleteVideo(ctx, removed.ID, repository.DeletionKindManual); err != nil {
		t.Fatal(err)
	}
	for _, v := range []*repository.Video{framed, removed} {
		if posters.Fetch(ctx, v.ID, v.Filename, srv.URL+"/a.jpg") {
			t.Fatalf("%s: fetch stored a poster", v.JobID)
		}
		if ok, _ := store.Exists(ctx, storagekeys.Snapshot(v.Filename, 0)); ok {
			t.Fatalf("%s: an object was written", v.JobID)
		}
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("image downloads = %d, want 0", hits)
	}
	if got, _ := repo.GetVideo(ctx, removed.ID); got.Thumbnail != nil {
		t.Fatal("a removed archive received a poster")
	}
}

type removedDuringFetch struct {
	repository.Repository
	videoID int64
	reads   int
}

func (r *removedDuringFetch) GetVideo(ctx context.Context, id int64) (*repository.Video, error) {
	r.reads++
	if r.reads == 1 {
		if err := r.SoftDeleteVideo(ctx, r.videoID, repository.DeletionKindManual); err != nil {
			return nil, err
		}
		v, err := r.Repository.GetVideo(ctx, id)
		if err != nil {
			return nil, err
		}
		v.DeletedAt = nil
		return v, nil
	}
	return r.Repository.GetVideo(ctx, id)
}

func TestFetch_RemovalDuringFetchLeavesNoPoster(t *testing.T) {
	ctx := context.Background()
	repo, store := posterFixture(t)
	srv := posterServer(t)
	v := seedArchive(t, repo, "job-1", "1001")
	posters := NewStore(&removedDuringFetch{Repository: repo, videoID: v.ID}, mediatest.New(t, &removedDuringFetch{Repository: repo, videoID: v.ID}, store, readyFunc(func(context.Context) error { return nil }), nil), srv.Client(), slog.New(slog.DiscardHandler))
	if posters.Fetch(ctx, v.ID, v.Filename, srv.URL+"/a.jpg") {
		t.Fatal("fetch stored a poster on a row removed meanwhile")
	}
	got, err := repo.GetVideo(ctx, v.ID)
	if err != nil || got.Thumbnail != nil || got.DeletedAt == nil {
		t.Fatalf("removed row after fetch = %+v, %v; want no poster", got, err)
	}
	if ok, _ := store.Exists(ctx, storagekeys.Snapshot(v.Filename, 0)); ok {
		t.Fatal("the unreferenced poster object was kept")
	}
}

func TestBackfill_VisitsEveryArchiveBeyondOnePage(t *testing.T) {
	ctx := context.Background()
	repo, store := posterFixture(t)
	cdn := posterServer(t)
	videos := map[string]twitch.Video{}
	var last *repository.Video
	for i := range 5 {
		id := fmt.Sprintf("%d", 100+i)
		last = seedArchive(t, repo, "job-"+id, id)
		url := "https://vod-secure.twitch.tv/_404/404_processing_%{width}x%{height}.png"
		if i == 4 {
			url = cdn.URL + "/poster-%{width}x%{height}.jpg"
		}
		videos[id] = twitch.Video{ID: id, ThumbnailURL: url}
	}
	helix := &fakeHelix{videos: videos}
	svc := New(NewStore(repo, mediatest.New(t, repo, store, readyFunc(func(context.Context) error { return nil }), nil), cdn.Client(), slog.New(slog.DiscardHandler)), repo, helix, slog.New(slog.DiscardHandler))
	svc.pageSize = 2
	report, err := svc.Backfill(ctx)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if report.Checked != 5 || report.Stored != 1 {
		t.Fatalf("report = %+v, want 5 checked, 1 stored", report)
	}
	if helix.calls != 3 {
		t.Fatalf("helix lookups = %d, want 3 pages", helix.calls)
	}
	got, _ := repo.GetVideo(ctx, last.ID)
	if got.Thumbnail == nil {
		t.Fatal("the archive beyond the first page did not get its poster")
	}
}

// TestBackfill_ResumesPastArchivesThatExhaustTheDeadline prevents hung poster
// hosts from starving later archives across repeated runs.
func TestBackfill_ResumesPastArchivesThatExhaustTheDeadline(t *testing.T) {
	repo, store := posterFixture(t)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "fast") {
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("\xff\xd8\xff\xe0jpeg"))
	}))
	defer slow.Close()
	videos := map[string]twitch.Video{}
	var ids []*repository.Video
	for i, name := range []string{"slow-a", "slow-b", "fast-c"} {
		id := fmt.Sprintf("%d", 200+i)
		ids = append(ids, seedArchive(t, repo, "job-"+id, id))
		videos[id] = twitch.Video{ID: id, ThumbnailURL: slow.URL + "/" + name + "-%{width}x%{height}.jpg"}
	}
	helix := &fakeHelix{videos: videos}
	svc := New(NewStore(repo, mediatest.New(t, repo, store, readyFunc(func(context.Context) error { return nil }), nil), slow.Client(), slog.New(slog.DiscardHandler)), repo, helix, slog.New(slog.DiscardHandler))
	svc.pageSize = 1

	run := func() (Report, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		return svc.Backfill(ctx)
	}
	for i := range 2 {
		report, err := run()
		if err != nil || report.Complete || report.Checked != 1 {
			t.Fatalf("run %d cut by the deadline = %+v, %v; want progress reported as success", i+1, report, err)
		}
	}
	report, err := run()
	if err != nil || !report.Complete || report.Stored != 1 {
		t.Fatalf("third run = %+v, %v; want the fast archive stored and the list finished", report, err)
	}
	got, _ := repo.GetVideo(context.Background(), ids[2].ID)
	if got.Thumbnail == nil {
		t.Fatal("the archive behind the slow ones never got its poster")
	}
	for _, v := range ids[:2] {
		if got, _ := repo.GetVideo(context.Background(), v.ID); got.Thumbnail != nil {
			t.Fatalf("%s: a hung download stored a poster", v.JobID)
		}
	}
	if svc.resumePoint() != 0 {
		t.Fatalf("resume point after a complete run = %d, want 0", svc.resumePoint())
	}

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.Backfill(expired); !errors.Is(err, context.Canceled) {
		t.Fatalf("run with an expired context = %v, want the cancellation surfaced", err)
	}
}

func TestFetchWaitsForWritableStorageAndRechecksAfterDownload(t *testing.T) {
	for _, verdict := range []error{storage.ErrUnattached, storage.ErrUnreachable, storage.ErrReadOnly, storage.ErrFull} {
		t.Run(verdict.Error(), func(t *testing.T) {
			repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
			ctx := t.Context()
			if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "bc-1", BroadcasterLogin: "bc-1", BroadcasterName: "bc-1"}); err != nil {
				t.Fatal(err)
			}
			v := seedArchive(t, repo, "paused-poster", "1")
			store, err := storage.NewLocal(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			var blocked atomic.Bool
			blocked.Store(true)
			gate := readyFunc(func(context.Context) error {
				if blocked.Load() {
					return verdict
				}
				return nil
			})
			var calls atomic.Int32
			var detach atomic.Bool
			detach.Store(true)
			cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				if detach.Load() {
					blocked.Store(true)
				}
				w.Header().Set("Content-Type", "image/jpeg")
				_, _ = io.WriteString(w, "jpeg")
			}))
			defer cdn.Close()
			posters := NewStore(repo, mediatest.New(t, repo, store, gate, nil), cdn.Client(), slog.New(slog.DiscardHandler))
			if posters.Fetch(ctx, v.ID, v.Filename, cdn.URL) || calls.Load() != 0 {
				t.Fatal("unavailable storage still fetched a poster")
			}
			blocked.Store(false)
			if posters.Fetch(ctx, v.ID, v.Filename, cdn.URL) || calls.Load() != 1 {
				t.Fatal("mid-download outage was ignored")
			}
			row, err := repo.GetVideo(ctx, v.ID)
			if err != nil {
				t.Fatal(err)
			}
			if row.Thumbnail != nil {
				t.Fatal("refused poster changed the row")
			}
			if ok, err := store.Exists(ctx, storagekeys.Snapshot(v.Filename, 0)); err != nil || ok {
				t.Fatalf("refused poster wrote an object: %v %v", ok, err)
			}
			detach.Store(false)
			blocked.Store(false)
			if !posters.Fetch(ctx, v.ID, v.Filename, cdn.URL) {
				t.Fatal("poster did not recover")
			}
		})
	}
}

func TestBackfillPausesBeforeHelixWhenStorageIsUnavailable(t *testing.T) {
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	helix := &fakeHelix{}
	svc := New(NewStore(repo, mediatest.New(t, repo, store, readyFunc(func(context.Context) error { return storage.ErrUnattached }), nil), http.DefaultClient, slog.New(slog.DiscardHandler)), repo, helix, slog.New(slog.DiscardHandler))
	report, err := svc.Backfill(t.Context())
	if err != nil || report.Complete || helix.calls != 0 {
		t.Fatalf("paused backfill: %+v %v calls=%d", report, err, helix.calls)
	}
}

func TestBackfillCancellationAfterProgressIsSurfaced(t *testing.T) {
	repo, store := posterFixture(t)
	v := seedArchive(t, repo, "interrupted-job", "123")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	}))
	defer host.Close()
	helix := &fakeHelix{videos: map[string]twitch.Video{"123": {ID: "123", ThumbnailURL: host.URL + "/poster.jpg"}}}
	log := slog.New(slog.DiscardHandler)
	svc := New(NewStore(repo, mediatest.New(t, repo, store, readyFunc(func(context.Context) error { return nil }), nil), host.Client(), log), repo, helix, log)
	report, err := svc.Backfill(ctx)
	if !errors.Is(err, context.Canceled) || report.Complete || report.Checked != 1 {
		t.Fatalf("cancelled backfill hid interruption: %+v, %v", report, err)
	}
	if svc.resumePoint() != v.ID {
		t.Fatalf("resume point=%d, want %d", svc.resumePoint(), v.ID)
	}
}

func (r frameRacer) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error { return fn(frameRacer{Repository: tx, videoID: r.videoID}) })
}
