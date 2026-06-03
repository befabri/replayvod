package video

import (
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

// TestWireEnumParity pins each wire-enum const to its repository source. The
// consts are defined as aliases, so a value can't silently drift; this fails
// loudly if a repository value is ever changed out from under the wire member.
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
