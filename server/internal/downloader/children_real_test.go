//go:build ffmpeg

package downloader

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/downloader/thumbnail"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/storage"
)

type snapshotFinishingAfterCancel struct {
	*storage.LocalStorage
	entered, cancelled, release chan struct{}
}

func (s *snapshotFinishingAfterCancel) Save(ctx context.Context, key string, body io.Reader) error {
	if strings.HasSuffix(key, "-snap00.jpg") {
		close(s.entered)
		<-ctx.Done()
		close(s.cancelled)
		<-s.release
		return s.LocalStorage.Save(context.WithoutCancel(ctx), key, body)
	}
	return s.LocalStorage.Save(ctx, key, body)
}

type titlePanicOnCancel struct{ started, panicked chan struct{} }

func (w titlePanicOnCancel) Watch(ctx context.Context, _ string, _ int64, _ streammeta.WatchInitial) {
	close(w.started)
	<-ctx.Done()
	close(w.panicked)
	panic("title poller cleanup panic")
}

type snapshotHTTP func(*http.Request) (*http.Response, error)

func (f snapshotHTTP) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRecordingJoinsRealSnapshotAndPanickingTitleChildrenBeforeSettlement(t *testing.T) {
	requireFFmpegHarness(t)
	edge := newTwitchEdge(t, twitchEdgeOpts{tsCount: 3, windowA: 3, baseSeqA: 100, aEndlist: 3})
	raw, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	held := &snapshotFinishingAfterCancel{LocalStorage: raw, entered: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	h := newHarnessServiceWithOpts(t, edge.URL(), harnessOpts{inheritedStorage: held, maxConcurrent: 1})
	s := h.svc
	t.Cleanup(func() { release.Do(func() { close(held.release) }); s.Shutdown() })
	seedArchiveChannel(t, h.repo, "children")
	s.cfg.ServerMode.Mode = config.ServerModePoll
	watcher := titlePanicOnCancel{started: make(chan struct{}), panicked: make(chan struct{})}
	s.metaWatcher = watcher
	s.hydrator = streammeta.NewHydrator(h.repo, nil, streammeta.Config{}, discardLog())
	s.snapshots = thumbnail.NewSnapshotter(thumbnail.SnapshotterConfig{HTTPClient: &http.Client{Transport: snapshotHTTP(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("preview bytes"))}, nil
	})}})
	jobID, err := s.Start(t.Context(), Params{BroadcasterID: "children", BroadcasterLogin: "children", DisplayName: "Children", Quality: repository.QualityHigh, Title: "Opening title"})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := h.repo.GetVideoByJobID(t.Context(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := h.repo.ListVideoMetadataChanges(t.Context(), initial.ID)
	if err != nil || len(metadata) != 1 || metadata[0].Title == nil || metadata[0].Title.Name != "Opening title" {
		t.Fatalf("Start lost opening metadata before claim: %+v, %v", metadata, err)
	}
	for name, signal := range map[string]<-chan struct{}{"snapshot publication": held.entered, "title polling": watcher.started, "title panic": watcher.panicked, "snapshot cancellation": held.cancelled} {
		select {
		case <-signal:
		case <-time.After(20 * time.Second):
			t.Fatalf("%s did not run", name)
		}
	}
	job, err := h.repo.GetJob(t.Context(), jobID)
	if err != nil || job.Status != repository.JobStatusRunning {
		t.Fatalf("terminal job published before child joined: %+v, %v", job, err)
	}
	v, err := h.repo.GetVideoByJobID(t.Context(), jobID)
	if err != nil || v.Status != repository.VideoStatusRunning || s.work.Used("live") != 1 {
		t.Fatalf("early settlement/release: %+v, %v", v, err)
	}
	release.Do(func() { close(held.release) })
	v = waitForVideoStatus(t, h.repo, v.ID, repository.VideoStatusDone, 20*time.Second)
	if v.Thumbnail != nil && strings.HasSuffix(*v.Thumbnail, "-snap00.jpg") {
		t.Fatalf("cancelled child promoted a late thumbnail: %+v", v.Thumbnail)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := s.work.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
}
