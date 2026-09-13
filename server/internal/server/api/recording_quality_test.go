package api

import (
	"testing"

	"github.com/befabri/replayvod/server/internal/server/api/schedule"
	"github.com/befabri/replayvod/server/internal/server/api/video"
	"github.com/befabri/replayvod/server/internal/validate"
)

func TestRecordingQualityInputValidation(t *testing.T) {
	for _, quality := range []string{"LOW", "MEDIUM", "HIGH", "1440", "BEST", "2160", "ULTRA"} {
		t.Run(quality, func(t *testing.T) {
			valid := quality != "2160" && quality != "ULTRA"
			for _, input := range []any{
				schedule.ScheduleSettingsInput{Quality: quality},
				video.TriggerDownloadInput{BroadcasterID: "123", Quality: quality},
			} {
				err := validate.V.Struct(input)
				if (err == nil) != valid {
					t.Fatalf("%T quality=%s: %v; valid=%t", input, quality, err, valid)
				}
			}
		})
	}
}
