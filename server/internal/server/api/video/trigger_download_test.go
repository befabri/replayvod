package video

import (
	"context"
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/trpcgo"
)

func TestTriggerDownload_MapsSafeDownloaderErrors(t *testing.T) {
	cases := []struct {
		name     string
		startErr error
		want     trpcgo.ErrorCode
	}{
		{"already recording this channel", downloader.ErrBusy, trpcgo.CodeConflict},
		{"at concurrent-download capacity", downloader.ErrAtCapacity, trpcgo.CodeConflict},
		{"server shutting down", downloader.ErrShuttingDown, trpcgo.CodeServiceUnavailable},
		{"unexpected error stays a 500", errors.New("disk on fire"), trpcgo.CodeInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeDownloadRepo{channel: &repository.Channel{
				BroadcasterID: "b1", BroadcasterLogin: "l", BroadcasterName: "N",
			}}
			svc := &DownloadService{
				repo:       repo,
				downloader: &fakeDownloadRunner{startErr: tc.startErr},
				log:        testClientLogger(),
			}
			h := &Handler{download: svc, log: testClientLogger()}
			ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u1"})

			_, err := h.TriggerDownload(ctx, TriggerDownloadInput{BroadcasterID: "b1"})
			var te *trpcgo.Error
			if !errors.As(err, &te) {
				t.Fatalf("err = %T (%v), want *trpcgo.Error", err, err)
			}
			if te.Code != tc.want {
				t.Fatalf("code = %v, want %v (msg %q)", te.Code, tc.want, te.Message)
			}
		})
	}
}
