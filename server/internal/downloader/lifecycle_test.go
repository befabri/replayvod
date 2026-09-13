package downloader

import (
	"context"
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/background"
	"github.com/befabri/replayvod/server/internal/repository"
)

func TestRecordingChildrenJoinBeforeSettlement(t *testing.T) {
	s := newTestService(t, t.TempDir())
	d := seedWebhookAttempt(t, s, "joined-recording")
	children := background.NewScope(t.Context())
	cancelled, release := make(chan struct{}), make(chan struct{})
	if err := children.Go("thumbnail promotion", false, func(ctx context.Context) error {
		<-ctx.Done()
		close(cancelled)
		<-release
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	d.stopChildren = func() { _ = children.Join() }
	done := make(chan struct{})
	go func() { defer close(done); s.failDownload(t.Context(), d, discardLog(), errors.New("capture failed")) }()
	<-cancelled
	v, err := s.repo.GetVideo(t.Context(), d.videoID)
	if err != nil || v.Status != repository.VideoStatusRunning {
		t.Fatalf("settled before writer exit: %+v %v", v, err)
	}
	close(release)
	<-done
	v, err = s.repo.GetVideo(t.Context(), d.videoID)
	if err != nil || v.Status != repository.VideoStatusFailed {
		t.Fatalf("settlement after join: %+v %v", v, err)
	}
}
