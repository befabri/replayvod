package video

import (
	"context"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func TestTriggerPreservesRecordingQuality(t *testing.T) {
	for _, tc := range []struct {
		quality string
		mode    string
		want    string
	}{
		{"", "video", repository.QualityHigh},
		{repository.QualityHigh, "video", repository.QualityHigh},
		{repository.Quality1440, "video", repository.Quality1440},
		{repository.QualityBest, "video", repository.QualityBest},
		{repository.QualityBest, "audio", repository.QualityBest},
	} {
		t.Run(tc.mode+"/"+tc.quality, func(t *testing.T) {
			runner := &fakeDownloadRunner{jobID: "quality-job"}
			svc := &DownloadService{
				repo: &fakeDownloadRepo{channel: &repository.Channel{
					BroadcasterID: "channel", BroadcasterLogin: "channel", BroadcasterName: "Channel",
				}},
				downloader: runner,
				log:        testClientLogger(),
			}
			_, err := svc.Trigger(context.Background(), TriggerInput{
				BroadcasterID: "channel", RecordingType: tc.mode, Quality: tc.quality, ForceH264: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if runner.params.Quality != tc.want || runner.params.RecordingType != tc.mode || runner.params.ForceH264 != (tc.mode == "video") {
				t.Fatalf("downloader settings = %+v; want %s/%s with video-only H.264 override", runner.params, tc.mode, tc.want)
			}
		})
	}
}
