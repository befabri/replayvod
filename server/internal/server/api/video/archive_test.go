package video

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/server/api/channel"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/befabri/trpcgo"
)

type fakeArchiveRepo struct {
	channels      map[string]*repository.Channel
	open          map[string]*repository.Video // by twitch video id
	liveByStream  map[string]*repository.Video // open live recordings by stream id
	streams       map[string]bool              // broadcasts the library knows, beyond liveByStream
	byJob         map[string]*repository.Video
	queue         []repository.Video
	failures      []repository.Video
	failuresSince time.Time
}

func (f *fakeArchiveRepo) GetChannel(_ context.Context, id string) (*repository.Channel, error) {
	if ch, ok := f.channels[id]; ok {
		return ch, nil
	}
	return nil, repository.ErrNotFound
}

func (f *fakeArchiveRepo) GetVideoByJobID(_ context.Context, jobID string) (*repository.Video, error) {
	if v, ok := f.byJob[jobID]; ok {
		return v, nil
	}
	return nil, repository.ErrNotFound
}

func (f *fakeArchiveRepo) GetOpenVideoByTwitchVideoID(_ context.Context, id string) (*repository.Video, error) {
	if v, ok := f.open[id]; ok {
		return v, nil
	}
	return nil, repository.ErrNotFound
}

func (f *fakeArchiveRepo) ListOpenVideosByTwitchVideoIDs(_ context.Context, ids []string) ([]repository.Video, error) {
	var out []repository.Video
	for _, id := range ids {
		if v, ok := f.open[id]; ok {
			row := *v
			vodID := id
			row.TwitchVideoID = &vodID
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeArchiveRepo) ListArchiveQueue(context.Context) ([]repository.Video, error) {
	return f.queue, nil
}

// GetStream knows every broadcast a live recording holds plus the ones listed
// in streams; anything else predates the install.
func (f *fakeArchiveRepo) GetStream(_ context.Context, id string) (*repository.Stream, error) {
	if _, ok := f.liveByStream[id]; ok || f.streams[id] {
		return &repository.Stream{ID: id}, nil
	}
	return nil, repository.ErrNotFound
}

func (f *fakeArchiveRepo) ListOpenVideosByStreamIDs(_ context.Context, ids []string) ([]repository.Video, error) {
	var out []repository.Video
	for _, id := range ids {
		if v, ok := f.liveByStream[id]; ok {
			row := *v
			sid := id
			row.StreamID = &sid
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeArchiveRepo) ListRecentArchiveFailures(_ context.Context, since time.Time, _ int) ([]repository.Video, error) {
	f.failuresSince = since
	return f.failures, nil
}

type fakeArchiveRunner struct {
	enqueued   []downloader.Params
	enqueueErr map[string]error // by vod id
	dequeueErr error
	dequeued   []int64
	retryErr   error
	retried    []int64
	cancelErr  error
	cancelled  []int64
	nextJob    int
	onEnqueue  func(p downloader.Params, jobID string)
}

func (f *fakeArchiveRunner) RetryArchive(_ context.Context, videoID int64) error {
	f.retried = append(f.retried, videoID)
	return f.retryErr
}

func (f *fakeArchiveRunner) CancelArchiveRetry(_ context.Context, videoID int64) error {
	f.cancelled = append(f.cancelled, videoID)
	return f.cancelErr
}

func (f *fakeArchiveRunner) EnqueueVOD(_ context.Context, p downloader.Params) (string, error) {
	if err, ok := f.enqueueErr[p.VODID]; ok {
		return "", err
	}
	f.nextJob++
	jobID := "job-" + p.VODID
	f.enqueued = append(f.enqueued, p)
	if f.onEnqueue != nil {
		f.onEnqueue(p, jobID)
	}
	return jobID, nil
}

func (f *fakeArchiveRunner) DequeueArchive(_ context.Context, videoID int64) error {
	f.dequeued = append(f.dequeued, videoID)
	return f.dequeueErr
}

type fakeHelix struct {
	users  map[string]twitch.User  // by login
	videos map[string]twitch.Video // by id
	byUser map[string][]twitch.Video
	// live maps a user id to the id of the stream it is broadcasting.
	live        map[string]string
	streamCalls []twitch.GetStreamsParams
	// batchNotFound makes a multi-id lookup answer 404 whenever any id is
	// unknown, which is how Helix behaves.
	batchNotFound bool
	calls         []twitch.GetVideosParams
	usersErr      error
}

func (f *fakeHelix) GetUsers(_ context.Context, p *twitch.GetUsersParams) ([]twitch.User, error) {
	if f.usersErr != nil {
		return nil, f.usersErr
	}
	var out []twitch.User
	for _, login := range p.Login {
		if u, ok := f.users[login]; ok {
			out = append(out, u)
		}
	}
	return out, nil
}

func (f *fakeHelix) GetVideos(_ context.Context, p *twitch.GetVideosParams) ([]twitch.Video, twitch.Pagination, error) {
	f.calls = append(f.calls, *p)
	if p.UserID != "" {
		return f.byUser[p.UserID], twitch.Pagination{Cursor: "next-" + p.After}, nil
	}
	var out []twitch.Video
	missing := false
	for _, id := range p.ID {
		if v, ok := f.videos[id]; ok {
			out = append(out, v)
		} else {
			missing = true
		}
	}
	if missing && (f.batchNotFound || len(p.ID) == 1) {
		return nil, twitch.Pagination{}, &twitch.HelixError{Status: http.StatusNotFound, Body: "vod not found"}
	}
	return out, twitch.Pagination{}, nil
}

func (f *fakeHelix) GetStreams(_ context.Context, p *twitch.GetStreamsParams) ([]twitch.Stream, twitch.Pagination, error) {
	f.streamCalls = append(f.streamCalls, *p)
	var out []twitch.Stream
	for _, id := range p.UserID {
		if sid, ok := f.live[id]; ok {
			out = append(out, twitch.Stream{ID: sid, UserID: id, Type: "live"})
		}
	}
	return out, twitch.Pagination{}, nil
}

type fakeSyncer struct {
	synced []string
	err    error
}

func (f *fakeSyncer) SyncFromTwitch(_ context.Context, in channel.SyncInput) (*repository.Channel, error) {
	f.synced = append(f.synced, in.BroadcasterID)
	if f.err != nil {
		return nil, f.err
	}
	return &repository.Channel{BroadcasterID: in.BroadcasterID, BroadcasterLogin: "synced_" + in.BroadcasterID, BroadcasterName: "Synced " + in.BroadcasterID}, nil
}

func helixVideo(id, userID, title string) twitch.Video {
	return twitch.Video{
		ID: id, UserID: userID, UserLogin: "login_" + userID, UserName: "Name " + userID,
		Title: title, CreatedAt: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC),
		URL: "https://www.twitch.tv/videos/" + id, Duration: "1h2m3s", Language: "fr", Type: "archive",
		ThumbnailURL: "https://cdn/thumb-%{width}x%{height}.jpg", ViewCount: 12,
	}
}

func newArchiveTestService(repo *fakeArchiveRepo, runner *fakeArchiveRunner, helix *fakeHelix, syncer *fakeSyncer) *ArchiveService {
	return &ArchiveService{repo: repo, downloader: runner, twitch: helix, channels: syncer, log: testClientLogger()}
}

func TestArchiveEnqueue_PerInputOutcomes(t *testing.T) {
	existing := &repository.Video{ID: 7, JobID: "job-existing", Title: "already here", Status: repository.VideoStatusDone}
	repo := &fakeArchiveRepo{
		channels: map[string]*repository.Channel{"u1": {BroadcasterID: "u1", BroadcasterLogin: "known", BroadcasterName: "Known"}},
		open:     map[string]*repository.Video{"300": existing},
		byJob:    map[string]*repository.Video{},
	}
	runner := &fakeArchiveRunner{}
	runner.onEnqueue = func(p downloader.Params, jobID string) {
		repo.byJob[jobID] = &repository.Video{ID: int64(100 + len(runner.enqueued)), JobID: jobID}
	}
	helix := &fakeHelix{
		videos: map[string]twitch.Video{
			"100": helixVideo("100", "u1", "known channel vod"),
			"200": helixVideo("200", "u2", "unknown channel vod"),
		},
		batchNotFound: true,
	}
	syncer := &fakeSyncer{}
	svc := newArchiveTestService(repo, runner, helix, syncer)

	items, err := svc.Enqueue(context.Background(), EnqueueInput{
		Inputs: []string{
			"https://www.twitch.tv/videos/100?t=10s", // queued, channel known
			"200",                                    // queued, channel synced first
			"https://www.twitch.tv/videos/300",       // already in the library
			"999",                                    // unknown on Twitch
			"not a link",                             // invalid
			"twitch.tv/videos/100",                   // duplicate of line 1
		},
		RecordingType: "audio", Quality: "", ForceH264: true, UserID: "user-1",
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	want := []EnqueueStatus{EnqueueQueued, EnqueueQueued, EnqueueExists, EnqueueNotFound, EnqueueInvalid, EnqueueInvalid}
	for i, w := range want {
		if items[i].Status != w {
			t.Errorf("item %d (%q) status = %q, want %q (%s)", i, items[i].Input, items[i].Status, w, items[i].Message)
		}
	}
	if items[0].VODID != "100" || items[0].JobID != "job-100" || items[0].VideoID != 101 || items[0].Title != "known channel vod" {
		t.Errorf("queued item = %+v", items[0])
	}
	if items[2].VideoID != 7 || items[2].JobID != "job-existing" || items[2].Title != "already here" {
		t.Errorf("exists item = %+v, want the library row", items[2])
	}
	if !strings.Contains(items[5].Message, "line 1") {
		t.Errorf("duplicate message = %q, want a pointer to line 1", items[5].Message)
	}
	if len(syncer.synced) != 1 || syncer.synced[0] != "u2" {
		t.Errorf("synced channels = %v, want only the unknown u2", syncer.synced)
	}
	if len(runner.enqueued) != 2 {
		t.Fatalf("enqueued = %d params, want 2", len(runner.enqueued))
	}
	p := runner.enqueued[0]
	if p.VODID != "100" || p.BroadcasterID != "u1" || p.BroadcasterLogin != "known" || p.DisplayName != "Known" {
		t.Errorf("params for 100 = %+v", p)
	}
	if p.Title != "known channel vod" || p.Language != "fr" || p.BroadcastAt == nil || p.BroadcastAt.Day() != 20 {
		t.Errorf("params metadata = title %q lang %q aired %v", p.Title, p.Language, p.BroadcastAt)
	}
	// Settings are normalized like a live trigger: audio drops force_h264,
	// empty quality becomes HIGH.
	if p.RecordingType != repository.RecordingTypeAudio || p.ForceH264 || p.Quality != repository.QualityHigh {
		t.Errorf("settings = %s/%v/%s, want audio/false/HIGH", p.RecordingType, p.ForceH264, p.Quality)
	}
	if runner.enqueued[1].BroadcasterLogin != "synced_u2" {
		t.Errorf("synced channel login = %q", runner.enqueued[1].BroadcasterLogin)
	}
	// The known VOD was answered from the library, so Helix never saw id 300,
	// and the 404 batch fell back to per-id lookups.
	for _, c := range helix.calls {
		for _, id := range c.ID {
			if id == "300" {
				t.Errorf("Helix was asked about an already-held VOD")
			}
		}
	}
	if len(helix.calls) < 2 {
		t.Errorf("helix calls = %d, want the batch plus per-id fallbacks", len(helix.calls))
	}
}

func TestArchiveEnqueue_RunnerErrors(t *testing.T) {
	repo := &fakeArchiveRepo{
		channels: map[string]*repository.Channel{"u1": {BroadcasterID: "u1", BroadcasterLogin: "known", BroadcasterName: "Known"}},
		open:     map[string]*repository.Video{"2": {ID: 42, JobID: "job-race"}},
		byJob:    map[string]*repository.Video{},
	}
	runner := &fakeArchiveRunner{enqueueErr: map[string]error{
		"1": errors.New("disk on fire"),
		"2": repository.ErrDuplicate,
		"3": downloader.ErrShuttingDown,
	}}
	helix := &fakeHelix{videos: map[string]twitch.Video{
		"1": helixVideo("1", "u1", "a"), "2": helixVideo("2", "u1", "b"), "3": helixVideo("3", "u1", "c"), "4": helixVideo("4", "u9", "d"),
	}}
	syncer := &fakeSyncer{err: errors.New("helix down")}
	svc := newArchiveTestService(repo, runner, helix, syncer)

	// "2" is held from the start here, so it never reaches the runner; simulate
	// the race instead by removing it from the open set for the pre-check.
	delete(repo.open, "2")
	items, err := svc.Enqueue(context.Background(), EnqueueInput{Inputs: []string{"1", "2", "3", "4"}, UserID: "u"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if items[0].Status != EnqueueError || items[0].Message == "" {
		t.Errorf("runner failure item = %+v, want error with a message", items[0])
	}
	if items[1].Status != EnqueueExists {
		t.Errorf("duplicate race item = %+v, want exists", items[1])
	}
	if items[2].Status != EnqueueError || !strings.Contains(items[2].Message, "restarting") {
		t.Errorf("shutting down item = %+v", items[2])
	}
	if items[3].Status != EnqueueError || !strings.Contains(items[3].Message, "sync") {
		t.Errorf("channel sync failure item = %+v", items[3])
	}
}

func TestArchiveListChannelVODs_HelixFailure(t *testing.T) {
	// A non-404 Helix error is not a per-item outcome: the call fails as a
	// whole so the dashboard shows one error instead of a page of guesses.
	helix := &fakeHelix{usersErr: &twitch.HelixError{Status: http.StatusInternalServerError, Body: "boom"}}
	svc := newArchiveTestService(&fakeArchiveRepo{}, &fakeArchiveRunner{}, helix, &fakeSyncer{})
	h := &Handler{archive: svc, log: testClientLogger()}
	ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u1"})
	if _, err := h.ListChannelVODs(ctx, ChannelVODsInput{Channel: "someone"}); !isTRPCCode(err, trpcgo.CodeInternalServerError) {
		t.Fatalf("helix 500 err = %v, want internal error", err)
	}
}

func TestArchiveListChannelVODs(t *testing.T) {
	repo := &fakeArchiveRepo{open: map[string]*repository.Video{
		"11": {ID: 5, Status: repository.VideoStatusRunning},
	}}
	helix := &fakeHelix{
		users:  map[string]twitch.User{"streamer": {ID: "u1", Login: "streamer", DisplayName: "Streamer", ProfileImageURL: "https://img"}},
		byUser: map[string][]twitch.Video{"u1": {helixVideo("11", "u1", "first"), helixVideo("12", "u1", "second")}},
	}
	svc := newArchiveTestService(repo, &fakeArchiveRunner{}, helix, &fakeSyncer{})
	h := &Handler{archive: svc, log: testClientLogger()}
	ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u1"})

	resp, err := h.ListChannelVODs(ctx, ChannelVODsInput{Channel: "https://www.twitch.tv/Streamer/videos", Cursor: "c1"})
	if err != nil {
		t.Fatalf("ListChannelVODs: %v", err)
	}
	if resp.Channel.BroadcasterID != "u1" || resp.Channel.Login != "streamer" || resp.Channel.Name != "Streamer" || resp.Channel.ProfileImageURL != "https://img" {
		t.Errorf("channel = %+v", resp.Channel)
	}
	if resp.NextCursor == nil || *resp.NextCursor != "next-c1" {
		t.Errorf("next cursor = %v, want next-c1", resp.NextCursor)
	}
	if len(resp.VODs) != 2 {
		t.Fatalf("vods = %d, want 2", len(resp.VODs))
	}
	first := resp.VODs[0]
	if first.ID != "11" || first.DurationSeconds != 3723 || first.ThumbnailURL != "https://cdn/thumb-320x180.jpg" || first.Type != "archive" {
		t.Errorf("first vod = %+v", first)
	}
	if first.ArchivedVideoID == nil || *first.ArchivedVideoID != 5 || first.ArchivedStatus == nil || *first.ArchivedStatus != VideoStatusRunning {
		t.Errorf("first vod archived marker = %v/%v, want 5/RUNNING", first.ArchivedVideoID, first.ArchivedStatus)
	}
	if resp.VODs[1].ArchivedVideoID != nil {
		t.Errorf("second vod should not be marked archived")
	}
	call := helix.calls[0]
	if call.UserID != "u1" || call.Type != "all" || call.Sort != "time" || call.First != "30" || call.After != "c1" {
		t.Errorf("helix videos params = %+v", call)
	}

	// Unknown channel and unusable input map to client errors.
	if _, err := h.ListChannelVODs(ctx, ChannelVODsInput{Channel: "nobody"}); !isTRPCCode(err, trpcgo.CodeNotFound) {
		t.Errorf("unknown channel err = %v, want not found", err)
	}
	if _, err := h.ListChannelVODs(ctx, ChannelVODsInput{Channel: "https://www.twitch.tv/videos/123"}); !isTRPCCode(err, trpcgo.CodeBadRequest) {
		t.Errorf("vod link as channel err = %v, want bad request", err)
	}
	if _, err := h.ListChannelVODs(context.Background(), ChannelVODsInput{Channel: "streamer"}); err == nil {
		t.Errorf("anonymous call succeeded")
	}
}

func TestArchiveDequeueAndQueueHandlers(t *testing.T) {
	runner := &fakeArchiveRunner{}
	svc := newArchiveTestService(&fakeArchiveRepo{}, runner, &fakeHelix{}, &fakeSyncer{})
	h := &Handler{archive: svc, log: testClientLogger()}
	ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u1"})

	if _, err := h.DequeueArchive(ctx, ArchiveVideoInput{VideoID: 9}); err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if len(runner.dequeued) != 1 || runner.dequeued[0] != 9 {
		t.Errorf("dequeued = %v", runner.dequeued)
	}
	runner.dequeueErr = downloader.ErrBusy
	if _, err := h.DequeueArchive(ctx, ArchiveVideoInput{VideoID: 9}); !isTRPCCode(err, trpcgo.CodeConflict) {
		t.Errorf("busy err = %v, want conflict", err)
	}
	runner.dequeueErr = repository.ErrNotFound
	if _, err := h.DequeueArchive(ctx, ArchiveVideoInput{VideoID: 9}); !isTRPCCode(err, trpcgo.CodeNotFound) {
		t.Errorf("missing err = %v, want not found", err)
	}
	if _, err := h.DequeueArchive(context.Background(), ArchiveVideoInput{VideoID: 9}); err == nil {
		t.Errorf("anonymous dequeue succeeded")
	}
	if _, err := h.EnqueueArchive(context.Background(), EnqueueArchiveInput{VODs: []string{"1"}}); err == nil {
		t.Errorf("anonymous enqueue succeeded")
	}

	if _, err := h.RetryArchive(ctx, ArchiveVideoInput{VideoID: 4}); err != nil || len(runner.retried) != 1 || runner.retried[0] != 4 {
		t.Errorf("retry = %v, retried %v", err, runner.retried)
	}
	for _, tc := range []struct {
		err  error
		code trpcgo.ErrorCode
	}{
		{downloader.ErrBusy, trpcgo.CodeConflict},
		{repository.ErrDuplicate, trpcgo.CodeConflict},
		{repository.ErrNotFound, trpcgo.CodeNotFound},
	} {
		runner.retryErr = tc.err
		if _, err := h.RetryArchive(ctx, ArchiveVideoInput{VideoID: 4}); !isTRPCCode(err, tc.code) {
			t.Errorf("retry with %v err = %v, want %v", tc.err, err, tc.code)
		}
	}
	if _, err := h.RetryArchive(context.Background(), ArchiveVideoInput{VideoID: 4}); err == nil {
		t.Errorf("anonymous retry succeeded")
	}
	if _, err := h.CancelArchiveRetry(ctx, ArchiveVideoInput{VideoID: 5}); err != nil || len(runner.cancelled) != 1 || runner.cancelled[0] != 5 {
		t.Errorf("cancel retry = %v, cancelled %v", err, runner.cancelled)
	}
	runner.cancelErr = repository.ErrNotFound
	if _, err := h.CancelArchiveRetry(ctx, ArchiveVideoInput{VideoID: 5}); !isTRPCCode(err, trpcgo.CodeNotFound) {
		t.Errorf("cancel retry without one err = %v, want not found", err)
	}
}

func TestArchiveQueueHandler_SplitsQueueAndRecentFailures(t *testing.T) {
	retryAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	repo := &fakeArchiveRepo{
		queue:    []repository.Video{{ID: 1, JobID: "j1", Status: repository.VideoStatusRunning, Source: repository.VideoSourceVOD}},
		failures: []repository.Video{{ID: 2, JobID: "j2", Status: repository.VideoStatusFailed, Source: repository.VideoSourceVOD, NextRetryAt: &retryAt}},
	}
	svc := newArchiveTestService(repo, &fakeArchiveRunner{}, &fakeHelix{}, &fakeSyncer{})
	now := time.Date(2026, 9, 8, 13, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	h := &Handler{archive: svc, video: New(sqliteadapter.New(testdb.NewSQLiteDB(t)), testClientLogger()), log: testClientLogger()}
	ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u1"})

	resp, err := h.ArchiveQueue(ctx)
	if err != nil {
		t.Fatalf("ArchiveQueue: %v", err)
	}
	if len(resp.Queue) != 1 || resp.Queue[0].ID != 1 || len(resp.Failures) != 1 || resp.Failures[0].ID != 2 {
		t.Fatalf("queue = %+v, failures = %+v", resp.Queue, resp.Failures)
	}
	if resp.Failures[0].NextRetryAt == nil || !resp.Failures[0].NextRetryAt.Equal(retryAt) {
		t.Fatalf("failure next_retry_at = %v, want %v", resp.Failures[0].NextRetryAt, retryAt)
	}
	if want := now.Add(-RecentFailureWindow); !repo.failuresSince.Equal(want) {
		t.Fatalf("failures listed since %v, want %v", repo.failuresSince, want)
	}
	if _, err := h.ArchiveQueue(context.Background()); err == nil {
		t.Errorf("anonymous queue read succeeded")
	}
}

func TestArchiveEnqueue_LivePrivateAndRecordedLive(t *testing.T) {
	completeStream, truncatedStream := "s-complete", "s-truncated"
	repo := &fakeArchiveRepo{
		channels: map[string]*repository.Channel{"u1": {BroadcasterID: "u1", BroadcasterLogin: "known", BroadcasterName: "Known"}},
		open:     map[string]*repository.Video{},
		liveByStream: map[string]*repository.Video{
			completeStream:  {ID: 40, JobID: "job-live-complete", Title: "recorded whole", Status: repository.VideoStatusDone},
			truncatedStream: {ID: 41, JobID: "job-live-truncated", Title: "recorded partly", Status: repository.VideoStatusDone, Truncated: true},
		},
		byJob: map[string]*repository.Video{},
	}
	runner := &fakeArchiveRunner{}
	runner.onEnqueue = func(p downloader.Params, jobID string) {
		repo.byJob[jobID] = &repository.Video{ID: int64(100 + len(runner.enqueued)), JobID: jobID}
	}
	live := helixVideo("10", "u1", "still on air")
	live.StreamID = "s-live"
	privateVOD := helixVideo("11", "u1", "subs only")
	privateVOD.Viewable = "private"
	recordedWhole := helixVideo("12", "u1", "recorded whole")
	recordedWhole.StreamID = completeStream
	recordedPartly := helixVideo("13", "u1", "recorded partly")
	recordedPartly.StreamID = truncatedStream
	plain := helixVideo("14", "u1", "plain")
	plain.StreamID = "s-unrecorded"
	helix := &fakeHelix{
		videos: map[string]twitch.Video{"10": live, "11": privateVOD, "12": recordedWhole, "13": recordedPartly, "14": plain},
		live:   map[string]string{"u1": "s-live"},
	}
	svc := newArchiveTestService(repo, runner, helix, &fakeSyncer{})

	items, err := svc.Enqueue(context.Background(), EnqueueInput{Inputs: []string{"10", "11", "12", "13", "14"}, UserID: "user-1"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	want := []EnqueueStatus{EnqueueLive, EnqueuePrivate, EnqueueExists, EnqueueQueued, EnqueueQueued}
	for i, w := range want {
		if items[i].Status != w {
			t.Errorf("item %d (%q) status = %q, want %q (%s)", i, items[i].Input, items[i].Status, w, items[i].Message)
		}
	}
	if !strings.Contains(items[0].Message, "still streaming") || items[0].Title != "still on air" {
		t.Errorf("live item = %+v", items[0])
	}
	if !strings.Contains(items[1].Message, "private") {
		t.Errorf("private item = %+v", items[1])
	}
	if items[2].VideoID != 40 || items[2].JobID != "job-live-complete" || items[2].Message != "recorded live" {
		t.Errorf("recorded-live item = %+v, want the live recording", items[2])
	}
	if items[3].Status != EnqueueQueued || !strings.Contains(items[3].Message, "cut short") {
		t.Errorf("truncated-recording VOD = %q %q, want queued with the cut-short explanation", items[3].Status, items[3].Message)
	}
	if len(runner.enqueued) != 2 || runner.enqueued[0].VODID != "13" || runner.enqueued[1].VODID != "14" {
		t.Fatalf("enqueued = %+v, want the truncated-recording VOD and the plain one", runner.enqueued)
	}
	if sid := runner.enqueued[0].StreamID; sid == nil || *sid != truncatedStream {
		t.Errorf("archive stream id = %v, want %q", sid, truncatedStream)
	}
	// A broadcast the library never saw has no streams row to reference, so
	// the archive is queued unlinked instead of failing the insert.
	if sid := runner.enqueued[1].StreamID; sid != nil {
		t.Errorf("archive of an unknown broadcast linked stream %q", *sid)
	}
	if len(helix.streamCalls) != 1 || len(helix.streamCalls[0].UserID) != 1 || helix.streamCalls[0].UserID[0] != "u1" {
		t.Errorf("live lookups = %+v, want one call for the distinct channel", helix.streamCalls)
	}
}

func TestArchiveListChannelVODs_MarksLiveAndRecordedLive(t *testing.T) {
	completeStream := "s-complete"
	repo := &fakeArchiveRepo{
		open:         map[string]*repository.Video{"11": {ID: 5, Status: repository.VideoStatusRunning}},
		liveByStream: map[string]*repository.Video{completeStream: {ID: 40, Status: repository.VideoStatusDone}},
	}
	archived := helixVideo("11", "u1", "archived")
	archived.StreamID = completeStream // an archive outranks the live match
	onAir := helixVideo("12", "u1", "on air")
	onAir.StreamID = "s-live"
	recorded := helixVideo("13", "u1", "recorded")
	recorded.StreamID = completeStream
	privateVOD := helixVideo("14", "u1", "private")
	privateVOD.Viewable = "private"
	helix := &fakeHelix{
		users:  map[string]twitch.User{"streamer": {ID: "u1", Login: "streamer", DisplayName: "Streamer"}},
		byUser: map[string][]twitch.Video{"u1": {archived, onAir, recorded, privateVOD}},
		live:   map[string]string{"u1": "s-live"},
	}
	svc := newArchiveTestService(repo, &fakeArchiveRunner{}, helix, &fakeSyncer{})
	h := &Handler{archive: svc, log: testClientLogger()}
	ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u1"})

	resp, err := h.ListChannelVODs(ctx, ChannelVODsInput{Channel: "streamer"})
	if err != nil {
		t.Fatalf("ListChannelVODs: %v", err)
	}
	if len(resp.VODs) != 4 {
		t.Fatalf("vods = %d", len(resp.VODs))
	}
	if v := resp.VODs[0]; v.HeldReason == nil || *v.HeldReason != ArchiveHeldByArchive || v.ArchivedVideoID == nil || *v.ArchivedVideoID != 5 {
		t.Errorf("archived vod = %+v", v)
	}
	if v := resp.VODs[1]; !v.Live || v.ArchivedVideoID != nil {
		t.Errorf("on-air vod = %+v, want live and unheld", v)
	}
	if v := resp.VODs[2]; v.Live || v.HeldReason == nil || *v.HeldReason != ArchiveHeldByLiveRecording || v.ArchivedVideoID == nil || *v.ArchivedVideoID != 40 || v.ArchivedStatus == nil || *v.ArchivedStatus != VideoStatusDone {
		t.Errorf("recorded-live vod = %+v", v)
	}
	if v := resp.VODs[3]; v.Viewable != "private" || v.ArchivedVideoID != nil {
		t.Errorf("private vod = %+v", v)
	}
}

func isTRPCCode(err error, code trpcgo.ErrorCode) bool {
	var te *trpcgo.Error
	return errors.As(err, &te) && te.Code == code
}

func (f *fakeArchiveRunner) PumpArchiveQueue(context.Context) {}
