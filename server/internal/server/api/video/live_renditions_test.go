package video

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/trpcgo"
)

func newRenditionsHandler(runner *fakeDownloadRunner, channel *repository.Channel) *Handler {
	svc := &DownloadService{
		repo:       &fakeDownloadRepo{channel: channel},
		downloader: runner,
		log:        testClientLogger(),
	}
	return &Handler{download: svc, log: testClientLogger()}
}

func TestLiveRenditions_ResolvesThroughTheChannelLogin(t *testing.T) {
	runner := &fakeDownloadRunner{renditions: downloader.LiveRenditions{
		Anonymous: true,
		Renditions: []downloader.Rendition{
			{Height: 1080, FPS: 60, Codec: twitch.CodecH264},
			{Height: 720, Codec: twitch.CodecH264},
		},
	}}
	h := newRenditionsHandler(runner, &repository.Channel{BroadcasterID: "b1", BroadcasterLogin: "altair"})

	got, err := h.LiveRenditions(context.Background(), LiveRenditionsInput{BroadcasterID: "b1", ForceH264: true})
	if err != nil {
		t.Fatal(err)
	}
	if runner.renditionsLogin != "altair" || !runner.renditionsForceH264 {
		t.Fatalf("resolved login=%q force_h264=%v, want altair under Force H.264", runner.renditionsLogin, runner.renditionsForceH264)
	}
	want := LiveRenditionsResponse{Anonymous: true, Renditions: []LiveRendition{
		{Height: 1080, FPS: 60, Codec: "h264"},
		{Height: 720, Codec: "h264"},
	}}
	if got.Anonymous != want.Anonymous || len(got.Renditions) != len(want.Renditions) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want.Renditions {
		if got.Renditions[i] != want.Renditions[i] {
			t.Fatalf("rendition %d = %+v, want %+v", i, got.Renditions[i], want.Renditions[i])
		}
	}
}

func TestLiveRenditions_EmptyListIsAnArrayNotNull(t *testing.T) {
	h := newRenditionsHandler(&fakeDownloadRunner{}, &repository.Channel{BroadcasterID: "b1", BroadcasterLogin: "altair"})
	got, err := h.LiveRenditions(context.Background(), LiveRenditionsInput{BroadcasterID: "b1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Renditions == nil || len(got.Renditions) != 0 {
		t.Fatalf("renditions = %#v, want empty slice", got.Renditions)
	}
}

func TestLiveRenditions_MapsErrors(t *testing.T) {
	cases := []struct {
		name    string
		channel *repository.Channel
		err     error
		want    trpcgo.ErrorCode
	}{
		{"channel not synced", nil, nil, trpcgo.CodeNotFound},
		{"channel offline", &repository.Channel{BroadcasterID: "b1", BroadcasterLogin: "l"}, &twitch.AuthError{Status: http.StatusNotFound}, trpcgo.CodeNotFound},
		{"other failure stays a 500", &repository.Channel{BroadcasterID: "b1", BroadcasterLogin: "l"}, errors.New("usher on fire"), trpcgo.CodeInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newRenditionsHandler(&fakeDownloadRunner{renditionsErr: tc.err}, tc.channel)
			_, err := h.LiveRenditions(context.Background(), LiveRenditionsInput{BroadcasterID: "b1"})
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

func TestTriggerDownload_PinnedHeightStoresItsTier(t *testing.T) {
	cases := []struct {
		name          string
		input         TriggerDownloadInput
		wantMaxHeight int
		wantQuality   string
	}{
		{"pins 936p under the HIGH tier", TriggerDownloadInput{BroadcasterID: "b1", Quality: "LOW", MaxHeight: 936}, 936, repository.QualityHigh},
		{"pins 160p under the LOW tier", TriggerDownloadInput{BroadcasterID: "b1", MaxHeight: 160}, 160, repository.QualityLow},
		{"audio ignores the pin and keeps the default", TriggerDownloadInput{BroadcasterID: "b1", RecordingType: "audio", MaxHeight: 1440}, 0, repository.QualityHigh},
		{"audio ignores the pin and keeps its own quality", TriggerDownloadInput{BroadcasterID: "b1", RecordingType: "audio", Quality: "LOW", MaxHeight: 1440}, 0, repository.QualityLow},
		{"no pin keeps the chosen tier", TriggerDownloadInput{BroadcasterID: "b1", Quality: "MEDIUM"}, 0, repository.QualityMedium},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeDownloadRunner{}
			h := newRenditionsHandler(runner, &repository.Channel{BroadcasterID: "b1", BroadcasterLogin: "l", BroadcasterName: "N"})
			ctx := middleware.WithUser(context.Background(), &repository.User{ID: "u1"})
			if _, err := h.TriggerDownload(ctx, tc.input); err != nil {
				t.Fatal(err)
			}
			if runner.params.MaxHeight != tc.wantMaxHeight || runner.params.Quality != tc.wantQuality {
				t.Fatalf("params max_height=%d quality=%q, want %d/%q", runner.params.MaxHeight, runner.params.Quality, tc.wantMaxHeight, tc.wantQuality)
			}
		})
	}
}
