package video

import (
	"context"
	"testing"

	"github.com/befabri/replayvod/server/internal/downloader"
)

type progressSubscribeRunner struct {
	ch <-chan downloader.Progress
}

func (r progressSubscribeRunner) Start(context.Context, downloader.Params) (string, error) {
	return "", nil
}

func (r progressSubscribeRunner) Cancel(string) {}

func (r progressSubscribeRunner) Subscribe(string) <-chan downloader.Progress {
	return r.ch
}

func (r progressSubscribeRunner) ListActiveProgress() []downloader.Progress {
	return nil
}

func (r progressSubscribeRunner) SubscribeActive(context.Context) <-chan struct{} {
	return nil
}

func TestDownloadProgressStreamsMediaOffsetSeconds(t *testing.T) {
	progress := make(chan downloader.Progress, 1)
	offset := 12.75
	progress <- downloader.Progress{
		JobID:              "job-1",
		PartIndex:          2,
		Stage:              "segments",
		SegmentsTotal:      -1,
		MediaOffsetSeconds: &offset,
	}
	close(progress)

	h := &Handler{
		download: &DownloadService{downloader: progressSubscribeRunner{ch: progress}},
	}
	events, err := h.DownloadProgress(context.Background(), DownloadProgressInput{JobID: "job-1"})
	if err != nil {
		t.Fatalf("DownloadProgress: %v", err)
	}

	got, ok := <-events
	if !ok {
		t.Fatal("DownloadProgress closed before emitting progress")
	}
	if got.MediaOffsetSeconds == nil || *got.MediaOffsetSeconds != offset {
		t.Fatalf("MediaOffsetSeconds=%v, want %v", got.MediaOffsetSeconds, offset)
	}
}
