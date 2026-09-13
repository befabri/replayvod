package video

import (
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func TestWireEnumParity(t *testing.T) {
	status := map[VideoStatus]string{
		VideoStatusPending: repository.VideoStatusPending,
		VideoStatusRunning: repository.VideoStatusRunning,
		VideoStatusDone:    repository.VideoStatusDone,
		VideoStatusFailed:  repository.VideoStatusFailed,
	}
	for got, want := range status {
		if string(got) != want {
			t.Errorf("VideoStatus %q != repository %q", got, want)
		}
	}

	intent := map[RecordingIntentStatus]string{
		RecordingIntentStatusActive:  repository.RecordingIntentStatusActive,
		RecordingIntentStatusWaiting: repository.RecordingIntentStatusWaiting,
		RecordingIntentStatusStopped: repository.RecordingIntentStatusStopped,
		RecordingIntentStatusExpired: repository.RecordingIntentStatusExpired,
	}
	for got, want := range intent {
		if string(got) != want {
			t.Errorf("RecordingIntentStatus %q != repository %q", got, want)
		}
	}

	completion := map[CompletionKind]string{
		CompletionKindComplete:  repository.CompletionKindComplete,
		CompletionKindPartial:   repository.CompletionKindPartial,
		CompletionKindCancelled: repository.CompletionKindCancelled,
	}
	for got, want := range completion {
		if string(got) != want {
			t.Errorf("CompletionKind %q != repository %q", got, want)
		}
	}

	source := map[VideoSource]string{
		VideoSourceLive: repository.VideoSourceLive,
		VideoSourceVOD:  repository.VideoSourceVOD,
	}
	for got, want := range source {
		if string(got) != want {
			t.Errorf("VideoSource %q != repository %q", got, want)
		}
	}

	playback := map[PlaybackAssetStatus]string{
		PlaybackAssetStatusBuilding:    repository.PlaybackAssetStatusBuilding,
		PlaybackAssetStatusReady:       repository.PlaybackAssetStatusReady,
		PlaybackAssetStatusFailed:      repository.PlaybackAssetStatusFailed,
		PlaybackAssetStatusUnavailable: repository.PlaybackAssetStatusUnavailable,
	}
	for got, want := range playback {
		if string(got) != want {
			t.Errorf("PlaybackAssetStatus %q != repository %q", got, want)
		}
	}
}
