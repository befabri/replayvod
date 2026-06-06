package waveform

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

func TestBuildPlanAllocatesExactTargetPoints(t *testing.T) {
	plan, ok := BuildPlan(42, "audio", nil, []PartInput{
		{Filename: "rec-part01.m4a", DurationSeconds: 9.5, SizeBytes: 10},
		{Filename: "rec-part02.m4a", DurationSeconds: 0.2, SizeBytes: 20},
		{Filename: "rec-part03.m4a", DurationSeconds: 0.3, SizeBytes: 30},
	})
	if !ok {
		t.Fatal("BuildPlan returned !ok")
	}
	wantPoints := PointCount(10, 3)
	gotPoints := 0
	for _, part := range plan.Parts {
		if part.Points < 1 {
			t.Fatalf("part %s points = %d, want at least 1", part.Filename, part.Points)
		}
		gotPoints += part.Points
	}
	if gotPoints != wantPoints {
		t.Fatalf("allocated points = %d, want %d", gotPoints, wantPoints)
	}
}

func TestBuildPlanDistributesMissingDurationsFromVideoDuration(t *testing.T) {
	total := 12.0
	plan, ok := BuildPlan(42, "audio", &total, []PartInput{
		{Filename: "rec-part01.m4a", DurationSeconds: 5, SizeBytes: 10},
		{Filename: "rec-part02.m4a", DurationSeconds: 0, SizeBytes: 20},
		{Filename: "rec-part03.m4a", DurationSeconds: 0, SizeBytes: 30},
	})
	if !ok {
		t.Fatal("BuildPlan returned !ok")
	}
	if plan.DurationSeconds != 12 {
		t.Fatalf("duration = %v, want 12", plan.DurationSeconds)
	}
	if plan.Parts[1].DurationSeconds != 3.5 || plan.Parts[2].DurationSeconds != 3.5 {
		t.Fatalf("missing durations = %v/%v, want 3.5/3.5", plan.Parts[1].DurationSeconds, plan.Parts[2].DurationSeconds)
	}
}

func TestBuildPlanDoesNotInflateZeroDurationPartWhenVideoDurationHasNoSlack(t *testing.T) {
	total := 100.0
	plan, ok := BuildPlan(42, "audio", &total, []PartInput{
		{Filename: "rec-part01.m4a", DurationSeconds: 100, SizeBytes: 10},
		{Filename: "rec-part02.m4a", DurationSeconds: 0, SizeBytes: 20},
	})
	if !ok {
		t.Fatal("BuildPlan returned !ok")
	}
	if plan.DurationSeconds != 100 {
		t.Fatalf("duration = %v, want 100", plan.DurationSeconds)
	}
	if plan.Parts[1].DurationSeconds != 0 {
		t.Fatalf("zero-duration part duration = %v, want 0", plan.Parts[1].DurationSeconds)
	}
	wantPoints := PointCount(100, 2)
	if plan.Parts[0].Points != wantPoints-1 || plan.Parts[1].Points != 1 {
		t.Fatalf("points = %d/%d, want %d/1", plan.Parts[0].Points, plan.Parts[1].Points, wantPoints-1)
	}
}

func TestBuildPlanDoesNotShrinkKnownDurationsWhenVideoDurationIsLower(t *testing.T) {
	total := 99.95
	plan, ok := BuildPlan(42, "audio", &total, []PartInput{
		{Filename: "rec-part01.m4a", DurationSeconds: 100, SizeBytes: 10},
		{Filename: "rec-part02.m4a", DurationSeconds: 0, SizeBytes: 20},
	})
	if !ok {
		t.Fatal("BuildPlan returned !ok")
	}
	if plan.DurationSeconds != 100 {
		t.Fatalf("duration = %v, want known part sum 100", plan.DurationSeconds)
	}
	if plan.Parts[1].DurationSeconds != 0 {
		t.Fatalf("zero-duration part duration = %v, want 0", plan.Parts[1].DurationSeconds)
	}
}

func TestArtifactRoundTripRejectsStaleFingerprint(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	key := storagekeys.Waveform("rec")
	resp := Response{DurationSeconds: 2, Peaks: []float32{0.1, 0.5}}
	if err := SaveArtifact(ctx, store, key, "fingerprint-a", resp); err != nil {
		t.Fatalf("SaveArtifact: %v", err)
	}

	got, ok, err := LoadArtifact(ctx, store, key, "fingerprint-a")
	if err != nil {
		t.Fatalf("LoadArtifact hit: %v", err)
	}
	if !ok {
		t.Fatal("LoadArtifact hit returned !ok")
	}
	if got.DurationSeconds != resp.DurationSeconds || len(got.Peaks) != len(resp.Peaks) || got.Peaks[1] != resp.Peaks[1] {
		t.Fatalf("loaded response = %+v, want %+v", got, resp)
	}

	if _, ok, err := LoadArtifact(ctx, store, key, "fingerprint-b"); err != nil || ok {
		t.Fatalf("stale LoadArtifact ok=%v err=%v, want miss without error", ok, err)
	}
}

func TestLoadArtifactTreatsFSErrNotExistAsMiss(t *testing.T) {
	missing := fsNotExistOnlyError{path: storagekeys.Waveform("missing-rec")}
	if !errors.Is(missing, fs.ErrNotExist) {
		t.Fatal("test error must satisfy errors.Is(_, fs.ErrNotExist)")
	}

	resp, ok, err := LoadArtifact(
		context.Background(),
		openErrorStorage{err: missing},
		storagekeys.Waveform("missing-rec"),
		"fingerprint",
	)
	if err != nil {
		t.Fatalf("LoadArtifact err = %v, want nil miss", err)
	}
	if ok {
		t.Fatalf("LoadArtifact ok=%v resp=%+v, want miss", ok, resp)
	}
}

func TestInputResolverUsesLocalFileBeforeStorage(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	if err := store.Save(ctx, storagekeys.Video("rec-part01.m4a"), bytes.NewReader([]byte("stored"))); err != nil {
		t.Fatalf("seed storage: %v", err)
	}
	localPath := filepath.Join(t.TempDir(), "rec-part01.m4a")
	if err := os.WriteFile(localPath, []byte("scratch"), 0o644); err != nil {
		t.Fatalf("write local: %v", err)
	}

	resolver := InputResolver{
		Storage: store,
		LocalFiles: map[string]string{
			"rec-part01.m4a": localPath,
		},
	}
	path, cleanup, err := resolver.Path(ctx, "rec-part01.m4a")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	defer cleanup()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read resolved path: %v", err)
	}
	if string(body) != "scratch" {
		t.Fatalf("resolved body = %q, want scratch", body)
	}
}

func TestReadPCM16PeaksHandlesOddChunkBoundary(t *testing.T) {
	var pcm bytes.Buffer
	for _, sample := range []int16{0, 16_384, -32_768, 8192} {
		if err := binary.Write(&pcm, binary.LittleEndian, sample); err != nil {
			t.Fatalf("write sample: %v", err)
		}
	}
	reader := oneByteReader{r: bytes.NewReader(pcm.Bytes())}
	peaks, err := ReadPCM16Peaks(reader, 4.0/SampleRate, 2)
	if err != nil {
		t.Fatalf("read peaks: %v", err)
	}
	if len(peaks) != 2 {
		t.Fatalf("peaks len = %d, want 2", len(peaks))
	}
	if peaks[0] != 0.5 {
		t.Fatalf("first peak = %v, want 0.5", peaks[0])
	}
	if peaks[1] != 1 {
		t.Fatalf("second peak = %v, want 1", peaks[1])
	}
}

type oneByteReader struct {
	r *bytes.Reader
}

type fsNotExistOnlyError struct {
	path string
}

func (e fsNotExistOnlyError) Error() string {
	return "not found: " + e.path
}

func (fsNotExistOnlyError) Is(target error) bool {
	return target == fs.ErrNotExist
}

type openErrorStorage struct {
	storage.Storage
	err error
}

func (s openErrorStorage) Open(context.Context, string) (io.ReadSeekCloser, error) {
	return nil, s.err
}

func (r oneByteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.r.Read(p)
}
