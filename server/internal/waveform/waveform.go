// Package waveform builds and persists audio waveform artifacts for audio-only
// recordings. The downloader writes the artifact when a recording finishes; the
// stream API serves it and can rebuild it if a historical recording predates the
// artifact.
package waveform

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/befabri/replayvod/server/internal/downloader/remux"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

const (
	SampleRate      = 8000
	PointsPerSecond = 8
	MinPoints       = 64
	MaxPoints       = 1600

	artifactVersion = 1
)

// Generator decodes one seekable audio file into normalized peak buckets. MP4
// and M4A containers often need seeks while demuxing, so non-local objects are
// materialized to a temp file before generation.
type Generator interface {
	Generate(ctx context.Context, inputPath string, durationSeconds float64, points int) ([]float32, error)
}

// Response is the public JSON shape served by /api/v1/videos/{id}/waveform.
type Response struct {
	DurationSeconds float64   `json:"duration_seconds"`
	Peaks           []float32 `json:"peaks"`
}

// Artifact is the storage JSON shape. The fingerprint lets readers reject a
// stale artifact after a recording is repaired or its parts are backfilled.
type Artifact struct {
	Version         int       `json:"version"`
	Fingerprint     string    `json:"fingerprint"`
	DurationSeconds float64   `json:"duration_seconds"`
	Peaks           []float32 `json:"peaks"`
}

func NewArtifact(fingerprint string, resp Response) Artifact {
	return Artifact{
		Version:         artifactVersion,
		Fingerprint:     fingerprint,
		DurationSeconds: resp.DurationSeconds,
		Peaks:           resp.Peaks,
	}
}

func (a Artifact) Matches(fingerprint string) bool {
	return a.Version == artifactVersion && a.Fingerprint == fingerprint
}

func (a Artifact) Response() Response {
	return Response{DurationSeconds: a.DurationSeconds, Peaks: a.Peaks}
}

// PartInput is the repository-neutral subset of a finalized video_part needed
// to plan a waveform.
type PartInput struct {
	Filename        string
	DurationSeconds float64
	SizeBytes       int64
}

type Part struct {
	Filename        string
	DurationSeconds float64
	SizeBytes       int64
	Points          int
}

type Plan struct {
	Fingerprint     string
	DurationSeconds float64
	Parts           []Part
}

func BuildPlan(videoID int64, recordingType string, videoDurationSeconds *float64, parts []PartInput) (Plan, bool) {
	if len(parts) == 0 {
		return Plan{}, false
	}
	durations, totalDuration := resolvedDurations(videoDurationSeconds, parts)
	if totalDuration <= 0 {
		return Plan{}, false
	}

	targetPoints := PointCount(totalDuration, len(parts))
	points := allocatePoints(durations, targetPoints)
	out := make([]Part, len(parts))
	for i, part := range parts {
		out[i] = Part{
			Filename:        part.Filename,
			DurationSeconds: durations[i],
			SizeBytes:       part.SizeBytes,
			Points:          points[i],
		}
	}
	plan := Plan{
		Fingerprint:     fingerprint(videoID, recordingType, out),
		DurationSeconds: totalDuration,
		Parts:           out,
	}
	return plan, true
}

func resolvedDurations(videoDurationSeconds *float64, parts []PartInput) ([]float64, float64) {
	durations := make([]float64, len(parts))
	positiveSum := 0.0
	missing := 0
	for i, part := range parts {
		if part.DurationSeconds > 0 {
			durations[i] = part.DurationSeconds
			positiveSum += part.DurationSeconds
		} else {
			missing++
		}
	}
	if missing == 0 {
		return durations, positiveSum
	}
	videoDuration := 0.0
	if videoDurationSeconds != nil && *videoDurationSeconds > 0 {
		videoDuration = *videoDurationSeconds
	}

	var fallback float64
	switch {
	case videoDuration > 0:
		fallback = math.Max(0, videoDuration-positiveSum) / float64(missing)
	case positiveSum > 0:
		fallback = positiveSum / float64(len(parts)-missing)
	default:
		return durations, 0
	}
	total := positiveSum
	for i := range durations {
		if durations[i] <= 0 {
			durations[i] = fallback
			total += fallback
		}
	}
	if videoDuration > total {
		total = videoDuration
	}
	return durations, total
}

func allocatePoints(durations []float64, targetPoints int) []int {
	points := make([]int, len(durations))
	if len(durations) == 0 {
		return points
	}
	if targetPoints < len(durations) {
		targetPoints = len(durations)
	}
	for i := range points {
		points[i] = 1
	}
	remaining := targetPoints - len(durations)
	if remaining == 0 {
		return points
	}

	totalDuration := 0.0
	for _, duration := range durations {
		totalDuration += duration
	}
	if totalDuration <= 0 {
		for i := 0; i < remaining; i++ {
			points[i%len(points)]++
		}
		return points
	}

	type remainder struct {
		index int
		value float64
	}
	remainders := make([]remainder, len(durations))
	allocated := 0
	for i, duration := range durations {
		exact := (duration / totalDuration) * float64(remaining)
		whole := int(math.Floor(exact))
		points[i] += whole
		allocated += whole
		remainders[i] = remainder{index: i, value: exact - float64(whole)}
	}
	sort.SliceStable(remainders, func(i, j int) bool {
		if remainders[i].value == remainders[j].value {
			return remainders[i].index < remainders[j].index
		}
		return remainders[i].value > remainders[j].value
	})
	for i := 0; i < remaining-allocated; i++ {
		points[remainders[i%len(remainders)].index]++
	}
	return points
}

func PointCount(durationSeconds float64, partCount int) int {
	points := int(math.Ceil(durationSeconds * PointsPerSecond))
	if points < MinPoints {
		points = MinPoints
	}
	if points > MaxPoints {
		points = MaxPoints
	}
	if points < partCount {
		points = partCount
	}
	return points
}

func fingerprint(videoID int64, recordingType string, parts []Part) string {
	var b strings.Builder
	fmt.Fprintf(&b, "v%d|%d|%s|", artifactVersion, videoID, recordingType)
	for _, part := range parts {
		fmt.Fprintf(&b, "%s:%0.3f:%d:%d;", part.Filename, part.DurationSeconds, part.SizeBytes, part.Points)
	}
	return b.String()
}

// InputResolver turns a stored part filename into a local path suitable for
// ffmpeg. Current downloader parts can be passed in LocalFiles so remote storage
// does not need to download bytes that still exist in scratch.
type InputResolver struct {
	Storage    storage.Storage
	LocalFiles map[string]string
}

func (r InputResolver) Path(ctx context.Context, filename string) (string, func() error, error) {
	if localPath := r.LocalFiles[filename]; localPath != "" {
		if _, err := os.Stat(localPath); err == nil {
			return localPath, func() error { return nil }, nil
		} else if !os.IsNotExist(err) {
			return "", nil, err
		}
	}
	if r.Storage == nil {
		return "", nil, fmt.Errorf("waveform input %s: storage unavailable", filename)
	}

	relPath := storagekeys.Video(filename)
	if local, ok := r.Storage.(*storage.LocalStorage); ok {
		p, err := local.LocalPath(relPath)
		if err != nil {
			return "", nil, err
		}
		if _, err := os.Stat(p); err != nil {
			return "", nil, err
		}
		return p, func() error { return nil }, nil
	}

	f, err := r.Storage.Open(ctx, relPath)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()

	ext := strings.ToLower(path.Ext(filename))
	if ext == "" {
		ext = ".m4a"
	}
	tmp, err := os.CreateTemp("", "replayvod-waveform-*"+ext)
	if err != nil {
		return "", nil, fmt.Errorf("create waveform temp file: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmp.Name())
		}
	}()

	if _, err := io.Copy(tmp, f); err != nil {
		tmp.Close()
		return "", nil, fmt.Errorf("copy waveform input: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", nil, fmt.Errorf("close waveform temp file: %w", err)
	}
	committed = true
	return tmp.Name(), func() error { return os.Remove(tmp.Name()) }, nil
}

func Generate(ctx context.Context, generator Generator, resolver InputResolver, plan Plan) (Response, error) {
	if generator == nil {
		return Response{}, fmt.Errorf("waveform generator unavailable")
	}
	peaks := make([]float32, 0, PointCount(plan.DurationSeconds, len(plan.Parts)))
	for _, part := range plan.Parts {
		inputPath, cleanup, err := resolver.Path(ctx, part.Filename)
		if err != nil {
			return Response{}, err
		}
		partPeaks, genErr := generator.Generate(ctx, inputPath, part.DurationSeconds, part.Points)
		cleanupErr := cleanup()
		if genErr != nil {
			return Response{}, genErr
		}
		if cleanupErr != nil {
			return Response{}, cleanupErr
		}
		peaks = append(peaks, NormalizePeaks(partPeaks, part.Points)...)
	}
	return Response{DurationSeconds: plan.DurationSeconds, Peaks: peaks}, nil
}

func LoadArtifact(ctx context.Context, store storage.Storage, key, fingerprint string) (Response, bool, error) {
	if store == nil {
		return Response{}, false, fmt.Errorf("waveform artifact storage unavailable")
	}
	f, err := store.Open(ctx, key)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Response{}, false, nil
		}
		return Response{}, false, err
	}
	defer f.Close()

	var artifact Artifact
	if err := json.NewDecoder(f).Decode(&artifact); err != nil {
		return Response{}, false, nil
	}
	if !artifact.Matches(fingerprint) {
		return Response{}, false, nil
	}
	return artifact.Response(), true, nil
}

func SaveArtifact(ctx context.Context, store storage.Storage, key, fingerprint string, resp Response) error {
	if store == nil {
		return fmt.Errorf("waveform artifact storage unavailable")
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(NewArtifact(fingerprint, resp)); err != nil {
		return fmt.Errorf("encode waveform artifact: %w", err)
	}
	if err := store.Save(ctx, key, &body); err != nil {
		return fmt.Errorf("save waveform artifact %s: %w", key, err)
	}
	return nil
}

// FFmpegGenerator decodes audio with ffmpeg into an 8 kHz mono PCM stream and
// buckets the peak amplitude.
type FFmpegGenerator struct {
	FFmpegPath string
}

func (g FFmpegGenerator) Generate(ctx context.Context, inputPath string, durationSeconds float64, points int) ([]float32, error) {
	if points <= 0 {
		return nil, nil
	}
	ffmpegPath := g.FFmpegPath
	if ffmpegPath == "" {
		ffmpegPath = remux.DefaultFFmpegPath
	}
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-i", inputPath,
		"-vn",
		"-ac", "1",
		"-ar", strconv.Itoa(SampleRate),
		"-f", "s16le",
		"pipe:1",
	}
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("waveform stdout pipe: %w", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("waveform ffmpeg start: %w", err)
	}
	peaks, readErr := ReadPCM16Peaks(stdout, durationSeconds, points)
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, fmt.Errorf("waveform read ffmpeg output: %w", readErr)
	}
	if waitErr != nil {
		return nil, fmt.Errorf("waveform ffmpeg failed: %w\nstderr:\n%s", waitErr, truncateStderr(stderr.String()))
	}
	return peaks, nil
}

func ReadPCM16Peaks(r io.Reader, durationSeconds float64, points int) ([]float32, error) {
	peaks := make([]float32, points)
	if points <= 0 {
		return peaks, nil
	}
	totalSamples := int(math.Ceil(durationSeconds * SampleRate))
	samplesPerBucket := int(math.Ceil(float64(totalSamples) / float64(points)))
	if samplesPerBucket < 1 {
		samplesPerBucket = 1
	}

	buf := make([]byte, 32*1024)
	var sample [2]byte
	bucket := 0
	inBucket := 0
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			for len(chunk) > 0 {
				if len(chunk) == 1 {
					sample[0] = chunk[0]
					if _, fillErr := io.ReadFull(r, sample[1:]); fillErr != nil {
						if fillErr == io.EOF || fillErr == io.ErrUnexpectedEOF {
							return peaks, nil
						}
						return nil, fillErr
					}
					processPCM16Sample(sample[:], peaks, &bucket, &inBucket, samplesPerBucket)
					chunk = nil
					continue
				}
				processPCM16Sample(chunk[:2], peaks, &bucket, &inBucket, samplesPerBucket)
				chunk = chunk[2:]
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return peaks, nil
}

func processPCM16Sample(sampleBytes []byte, peaks []float32, bucket, inBucket *int, samplesPerBucket int) {
	if len(peaks) == 0 {
		return
	}
	if *bucket < len(peaks) {
		sample := int16(binary.LittleEndian.Uint16(sampleBytes))
		amp := float32(math.Abs(float64(sample)) / 32768)
		if amp > peaks[*bucket] {
			if amp > 1 {
				amp = 1
			}
			peaks[*bucket] = amp
		}
	}
	*inBucket = *inBucket + 1
	if *inBucket >= samplesPerBucket {
		*bucket++
		*inBucket = 0
	}
}

func NormalizePeaks(peaks []float32, points int) []float32 {
	out := make([]float32, points)
	copy(out, peaks[:min(len(peaks), points)])
	return out
}

func truncateStderr(s string) string {
	const limit = 4 << 10
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "\n... truncated ..."
}
