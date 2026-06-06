package downloader

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/waveform"
)

type fakeDownloaderWaveformGenerator struct {
	bodies []string
}

func (f *fakeDownloaderWaveformGenerator) Generate(_ context.Context, inputPath string, _ float64, points int) ([]float32, error) {
	body, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, err
	}
	f.bodies = append(f.bodies, string(body))
	peaks := make([]float32, points)
	for i := range peaks {
		peaks[i] = 0.25
	}
	return peaks, nil
}

func TestPersistAudioWaveformWritesArtifactFromScratchFiles(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	if err := store.Save(ctx, storagekeys.Video("rec-part01.m4a"), strings.NewReader("stored")); err != nil {
		t.Fatalf("seed storage: %v", err)
	}
	scratch := filepath.Join(t.TempDir(), "rec-part01.m4a")
	if err := os.WriteFile(scratch, []byte("scratch"), 0o644); err != nil {
		t.Fatalf("write scratch: %v", err)
	}
	generator := &fakeDownloaderWaveformGenerator{}
	svc := &Service{storage: store, waveforms: generator}

	err = svc.persistAudioWaveform(ctx, 42, "rec", "audio", 2, []partResult{
		{
			filename:        "rec-part01.m4a",
			localPath:       scratch,
			durationSeconds: 2,
			sizeBytes:       7,
		},
	})
	if err != nil {
		t.Fatalf("persistAudioWaveform: %v", err)
	}
	if len(generator.bodies) != 1 || generator.bodies[0] != "scratch" {
		t.Fatalf("generator bodies = %#v, want scratch file", generator.bodies)
	}

	plan, ok := waveform.BuildPlan(42, "audio", ptrFloat64(2), []waveform.PartInput{
		{Filename: "rec-part01.m4a", DurationSeconds: 2, SizeBytes: 7},
	})
	if !ok {
		t.Fatal("BuildPlan returned !ok")
	}
	resp, hit, err := waveform.LoadArtifact(ctx, store, storagekeys.Waveform("rec"), plan.Fingerprint)
	if err != nil {
		t.Fatalf("LoadArtifact: %v", err)
	}
	if !hit {
		t.Fatal("LoadArtifact returned miss")
	}
	if resp.DurationSeconds != 2 || len(resp.Peaks) != waveform.MinPoints {
		t.Fatalf("artifact response = %+v, want duration 2 and %d peaks", resp, waveform.MinPoints)
	}
}

func ptrFloat64(v float64) *float64 { return &v }
