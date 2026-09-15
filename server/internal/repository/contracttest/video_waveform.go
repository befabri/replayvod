package contracttest

import (
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testVideoWaveformKeyRoundTrip(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "execution-channel")
	a, err := repo.CreateVideo(ctx, executionInput("waveform-a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.CreateVideo(ctx, executionInput("waveform-b"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetVideoWaveformKey(ctx, a.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing waveform: %v", err)
	}
	if err := repo.SetVideoWaveformKey(ctx, a.ID+b.ID+1, "waveforms/orphan.json"); err == nil {
		t.Fatal("waveform stored for a missing video")
	}
	if err := repo.SetVideoWaveformKey(ctx, a.ID, "waveforms/a-1.json"); err != nil {
		t.Fatal(err)
	}
	if key, err := repo.GetVideoWaveformKey(ctx, a.ID); err != nil || key != "waveforms/a-1.json" {
		t.Fatalf("waveform = %q, %v", key, err)
	}
	if err := repo.SetVideoWaveformKey(ctx, a.ID, "waveforms/a-2.json"); err != nil {
		t.Fatal(err)
	}
	if key, err := repo.GetVideoWaveformKey(ctx, a.ID); err != nil || key != "waveforms/a-2.json" {
		t.Fatalf("replaced waveform = %q, %v", key, err)
	}
	if err := repo.SetVideoWaveformKey(ctx, b.ID, "waveforms/a-2.json"); !errors.Is(err, repository.ErrDuplicate) {
		t.Fatalf("one key served two recordings: %v", err)
	}
	if _, err := repo.GetVideoWaveformKey(ctx, b.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("rejected write stored a waveform: %v", err)
	}
	if err := repo.SetVideoWaveformKey(ctx, b.ID, "waveforms/b-1.json"); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteVideoWaveformKey(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetVideoWaveformKey(ctx, a.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("deleted waveform: %v", err)
	}
	if err := repo.DeleteVideoWaveformKey(ctx, a.ID); err != nil {
		t.Fatalf("deleting a missing waveform: %v", err)
	}
	if key, err := repo.GetVideoWaveformKey(ctx, b.ID); err != nil || key != "waveforms/b-1.json" {
		t.Fatalf("other waveform after delete = %q, %v", key, err)
	}
	if err := repo.SetVideoWaveformKey(ctx, b.ID, "waveforms/a-2.json"); err != nil {
		t.Fatalf("released key not reusable: %v", err)
	}
}
