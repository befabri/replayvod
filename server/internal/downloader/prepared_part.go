package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/befabri/replayvod/server/internal/mediastore"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

// PreparedPart is an immutable publication candidate, retained in the attempt
// checkpoint until its part boundary or terminal settlement is committed.
type PreparedPart struct {
	Filename      string                       `json:"filename"`
	Path          string                       `json:"path"`
	Digest        string                       `json:"digest"`
	Facts         repository.VideoPartFinalize `json:"facts"`
	Thumbnail     string                       `json:"thumbnail,omitempty"`
	ThumbnailPath string                       `json:"thumbnail_path,omitempty"`
	Strip         string                       `json:"strip,omitempty"`
	StripPath     string                       `json:"strip_path,omitempty"`
}
type checkedReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r checkedReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func preparedDigest(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, checkedReader{ctx, f}); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func (s *Service) publishPreparedPart(ctx context.Context, d *download, p *PreparedPart, log *slog.Logger) (*partResult, error) {
	if p.Digest == "" {
		d.persistenceErr = mediastore.ErrContentChanged
		return nil, d.persistenceErr
	}
	for {
		err := s.uploadScratch(ctx, d, p.Path, storagekeys.Video(p.Filename), p.Digest)
		if err == nil {
			break
		}
		if ctx.Err() != nil || errors.Is(err, repository.ErrStaleExecution) || errors.Is(err, repository.ErrStopRequested) || errors.Is(err, mediastore.ErrContentChanged) || errors.Is(err, os.ErrNotExist) {
			d.persistenceErr = err
			return nil, err
		}
		log.Warn("prepared part publication deferred", "error", err)
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			d.persistenceErr = err
			return nil, err
		case <-timer.C:
		}
	}
	facts := p.Facts
	if p.Thumbnail != "" {
		if err := s.uploadFromScratch(ctx, d, p.ThumbnailPath, p.Thumbnail); err == nil {
			facts.Thumbnail = &p.Thumbnail
		} else {
			log.Warn("thumbnail publication failed", "error", err)
		}
	}
	if p.Strip != "" {
		if err := s.uploadFromScratch(ctx, d, p.StripPath, p.Strip); err != nil {
			log.Warn("strip publication failed", "error", err)
		}
	}
	if err := s.recordPart(ctx, d, &facts); err != nil {
		d.persistenceErr = err
		return nil, err
	}
	log.Info("part complete",
		"part_index", d.resume.CurrentPartIndex,
		"duration_seconds", facts.DurationSeconds,
		"size_bytes", facts.SizeBytes,
		"source_bytes", d.resume.PartBytes,
	)
	if partOutgrewSource(facts.SizeBytes, d.resume.PartBytes, s.cfg.App.Download.MaxPartBytes) {
		log.Warn("remuxed part is larger than the source segments the size ceiling counted; allow extra margin below an external file size limit",
			"part_index", d.resume.CurrentPartIndex,
			"size_bytes", facts.SizeBytes,
			"source_bytes", d.resume.PartBytes,
			"max_part_bytes", s.cfg.App.Download.MaxPartBytes,
		)
	}
	out := &partResult{filename: p.Filename, localPath: p.Path, durationSeconds: facts.DurationSeconds, sizeBytes: facts.SizeBytes}
	if facts.Thumbnail != nil {
		out.thumbRel = *facts.Thumbnail
	}
	return out, nil
}
