package playbackcache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/befabri/replayvod/server/internal/downloader/remux"
	"github.com/befabri/replayvod/server/internal/mediastore"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

// preparedArtifact owns only scratch data until publication has exclusive
// ownership of the recording. Cleanup cannot follow a swapped storage root.
type preparedArtifact struct {
	path      string
	size      int64
	workspace *mediastore.Workspace
}

func (a *preparedArtifact) cleanup() { _ = a.workspace.Close(true) }

func (s *Service) buildArtifact(ctx context.Context, parts []repository.VideoPart) (artifact *preparedArtifact, err error) {
	workspace, err := s.store.Scratch().New("playback", expectedSize(parts)*2+expectedSize(parts)/buildOvershootMarginDivisor)
	if err != nil {
		return nil, err
	}
	defer func() {
		// Ownership transfers only when an artifact is returned. A panic is
		// recovered by the runner after this frame unwinds, with err still nil.
		if artifact == nil {
			_ = workspace.Close(true)
		}
	}()
	workDir := workspace.Dir
	ctx, stop := workspace.Monitor(ctx)
	defer stop()
	localParts, err := s.localPartPaths(ctx, workspace, parts)
	if err != nil {
		return nil, err
	}

	listPath := filepath.Join(workDir, "parts.txt")
	if err := remux.WriteConcatListFile(listPath, localParts); err != nil {
		return nil, err
	}

	// Concat can outlive the verified mount, so its output stays in scratch until
	// publication rechecks storage identity.
	outputPath := filepath.Join(workDir, "playback"+partExtension(parts[0]))
	if err := s.runner.Concat(ctx, listPath, outputPath); err != nil {
		return nil, err
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return nil, fmt.Errorf("stat playback artifact: %w", err)
	}
	return &preparedArtifact{path: outputPath, size: info.Size(), workspace: workspace}, nil
}

func (s *Service) publishArtifact(ctx context.Context, owned *mediastore.Recording, filename string, artifact *preparedArtifact) error {
	f, err := os.Open(artifact.path)
	if err != nil {
		return fmt.Errorf("open playback artifact: %w", err)
	}
	defer f.Close()
	if err := s.storageReady(ctx); err != nil {
		return err
	}
	if err := owned.Save(ctx, storagekeys.Video(filename), f); err != nil {
		return fmt.Errorf("save playback artifact: %w", err)
	}
	return nil
}

func (s *Service) localPartPaths(ctx context.Context, workspace *mediastore.Workspace, parts []repository.VideoPart) ([]string, error) {
	out := make([]string, 0, len(parts))
	for i, part := range parts {
		name := fmt.Sprintf("part%03d%s", i+1, partExtension(part))
		p, err := workspace.Copy(ctx, s.store, storagekeys.Video(part.Filename), name)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func orderedParts(parts []repository.VideoPart) []repository.VideoPart {
	out := append([]repository.VideoPart(nil), parts...)
	slices.SortFunc(out, func(a, b repository.VideoPart) int {
		return int(a.PartIndex) - int(b.PartIndex)
	})
	return out
}

func canCopyConcat(parts []repository.VideoPart) (bool, string) {
	if len(parts) < 2 {
		return false, "recording has fewer than two parts"
	}
	first := parts[0]
	ext := partExtension(first)
	if ext != ".mp4" && ext != ".m4a" {
		return false, "only MP4/M4A parts can be copy-concatenated"
	}
	for i, part := range parts {
		if part.PartIndex != int32(i+1) {
			return false, "part indexes are not contiguous"
		}
		if part.SizeBytes <= 0 {
			return false, "one or more parts have no stored bytes"
		}
		if partExtension(part) != ext {
			return false, "parts use different container extensions"
		}
		if part.Quality != first.Quality || part.Codec != first.Codec || part.SegmentFormat != first.SegmentFormat || !fpsEqual(part.FPS, first.FPS) {
			return false, "parts do not share the same recorded rendition metadata"
		}
	}
	return true, ""
}

func fpsEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	diff := *a - *b
	if diff < 0 {
		diff = -diff
	}
	return diff < 0.001
}

func partExtension(part repository.VideoPart) string {
	return strings.ToLower(filepath.Ext(part.Filename))
}

func totalDuration(parts []repository.VideoPart) float64 {
	var total float64
	for _, part := range parts {
		total += part.DurationSeconds
	}
	return total
}

func expectedSize(parts []repository.VideoPart) int64 {
	var total int64
	for _, part := range parts {
		total += part.SizeBytes
	}
	return total
}

func mimeTypeForExtension(ext string) string {
	if ext == ".m4a" {
		return "audio/mp4"
	}
	return "video/mp4"
}

// remuxRunner shares the recording pipeline's escaping and atomic output commit.
type remuxRunner struct {
	remuxer *remux.Remuxer
}

func (r remuxRunner) Concat(ctx context.Context, listPath, outputPath string) error {
	kind := remux.KindVideo
	if strings.EqualFold(filepath.Ext(outputPath), ".m4a") {
		kind = remux.KindAudio
	}
	return r.remuxer.Run(ctx, remux.RunInput{
		Mode:           remux.ModeTS,
		Kind:           kind,
		Faststart:      true,
		InputPath:      listPath,
		OutputDir:      filepath.Dir(outputPath),
		OutputBasename: strings.TrimSuffix(filepath.Base(outputPath), filepath.Ext(outputPath)),
	})
}
