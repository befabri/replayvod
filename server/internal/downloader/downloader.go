// Package downloader runs the native Go HLS → ffmpeg pipeline that
// turns a live Twitch stream into a stored MP4. Each download has
// a unique jobID so callers (tRPC handlers, the scheduler, webhook
// handlers) can subscribe to progress updates and request
// cancellation without holding a reference to the in-flight work.
//
// Pipeline composition (spec stages 1-11):
//
//	1. twitch.Client.PlaybackToken            — GQL access token
//	2. twitch.Client.FetchMasterPlaylist      — usher manifest
//	3. twitch.SelectVariant                   — quality/codec pick
//	4. hls.Run                                — segments → scratch
//	5. remux.PrepareInput                     — segments.txt / media.m3u8
//	6. remux.Remuxer.Run                      — ffmpeg → mp4/m4a
//	7. probe.Probe.Run                        — duration + streams
//	8. thumbnail.Generator.Generate           — jpg at 10% (video only)
//	9. corruption check → remux.Remuxer.Heal  — if duration drifts >50s
//	10. storage.Save                          — upload to backend
//	11. os.RemoveAll(work_dir)                — cleanup
//
// Phase 6a scope: single-part happy path. Auth refresh (ErrPlaylistAuth
// → re-stages), resume-on-restart, part-splitting on variant/codec/
// container change, and stitched-ad gap segregation are Phase 6b+
// concerns.
package downloader

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/downloader/hls"
	"github.com/befabri/replayvod/server/internal/downloader/probe"
	"github.com/befabri/replayvod/server/internal/downloader/remux"
	"github.com/befabri/replayvod/server/internal/downloader/thumbnail"
	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
)

// qualityToHeight maps the repository's coarse Quality enum (LOW /
// MEDIUM / HIGH) to the numeric-string form the twitch variant
// selector expects. Unknown quality values default to 1080 — the
// spec's PreferredQuality — so a config drift doesn't silently
// pick an unexpected variant.
func qualityToHeight(q string) string {
	switch q {
	case repository.QualityHigh:
		return "1080"
	case repository.QualityMedium:
		return "720"
	case repository.QualityLow:
		return "480"
	default:
		return "1080"
	}
}

// Params describes a single download request. RecordingType +
// ForceH264 drive Stage 3 variant selection and land on the video
// row; zero values are the conservative defaults (video + no
// codec-override).
type Params struct {
	BroadcasterID    string
	BroadcasterLogin string
	DisplayName      string
	Quality          string
	Language         string
	ViewerCount      int64
	StreamID         *string

	// RecordingType is "video" (default) or "audio". Audio jobs
	// pick the audio_only rendition at Stage 3 and produce an
	// .m4a output. Empty defaults to video at the repo layer.
	RecordingType string

	// ForceH264 drops HEVC/AV1 variants before the Stage 3
	// quality-fallback chain. Operator-exposed per the spec's
	// "User codec preference" section.
	ForceH264 bool
}

// Progress is a single snapshot written to the per-job progress
// channel. The Stage field tracks which spec stage is running so
// the SSE subscriber can show a meaningful label.
type Progress struct {
	JobID   string
	Stage   string  // "auth" | "playlist" | "segments" | "remux" | "metadata" | "thumbnail" | "done"
	Percent float64 // -1 when not computable (live stream, in-progress remux)
	Speed   string
	ETA     string
}

// Service orchestrates downloads. Safe for concurrent use. One
// Service per process; the pipeline components are constructed
// once in NewService and shared across all jobs.
type Service struct {
	cfg     *config.Config
	repo    repository.Repository
	storage storage.Storage
	log     *slog.Logger

	twitch  *twitch.Client
	fetcher *hls.Fetcher
	remuxer *remux.Remuxer
	probe   *probe.Probe
	thumb   *thumbnail.Generator

	mu     sync.Mutex
	active map[string]*download
}

// download is the per-job state kept in memory. cancel propagates
// a user Cancel() to every stage (playlist, fetch, remux, probe,
// thumbnail) via one shared ctx.
type download struct {
	jobID         string
	videoID       int64
	broadcasterID string
	cancel        context.CancelFunc
	userCancelled bool
	progressCh    chan Progress
	startedAt     time.Time
}

// NewService wires up the pipeline components. The twitch client,
// fetcher, remuxer, probe, and thumbnail generator are all
// process-lifetime singletons — they hold no per-job state.
func NewService(cfg *config.Config, repo repository.Repository, store storage.Storage, log *slog.Logger) *Service {
	domainLog := log.With("domain", "downloader")

	tw := twitch.New(twitch.Config{
		ServiceAccountRefreshToken: cfg.Env.ServiceAccountOAuthToken,
	}, domainLog)

	// Shared HTTP client for segment fetches. MaxConnsPerHost is
	// the service-wide cap on concurrent Twitch edge connections;
	// spec Stage 4 sizes it as MaxConcurrent × SegmentConcurrency.
	aggregateHostCap := max(1, cfg.App.Download.MaxConcurrent) * max(1, cfg.App.Download.SegmentConcurrency)
	segTransport := &http.Transport{
		MaxConnsPerHost:       aggregateHostCap,
		MaxIdleConnsPerHost:   aggregateHostCap,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		DisableCompression:    true,
	}
	segClient := &http.Client{Transport: segTransport}

	fetcher := hls.NewFetcher(segClient, hls.FetcherConfig{
		TransportAttempts:   cfg.App.Download.NetworkAttempts,
		ServerErrorAttempts: cfg.App.Download.ServerErrorAttempts,
		CDNLagAttempts:      cfg.App.Download.CDNLagAttempts,
	}, domainLog)

	s := &Service{
		cfg:     cfg,
		repo:    repo,
		storage: store,
		log:     domainLog,
		twitch:  tw,
		fetcher: fetcher,
		remuxer: &remux.Remuxer{Log: domainLog},
		probe:   &probe.Probe{Log: domainLog},
		thumb:   &thumbnail.Generator{Log: domainLog},
		active:  make(map[string]*download),
	}
	s.sweepOrphanedTemps()
	return s
}

// sweepOrphanedTemps removes leftover per-job work directories
// from a previous crash or hard kill. The native pipeline's
// partial output is never resumable at Phase 6a (resume lands in
// 6b), so cleanup is always safe.
//
// Scratch layout: <scratch>/<jobID>/ contains segments/, the
// remuxed mp4, and the thumbnail. One RemoveAll per job dir
// gets everything.
func (s *Service) sweepOrphanedTemps() {
	scratch := s.cfg.Env.ScratchDir
	entries, err := os.ReadDir(scratch)
	if err != nil {
		return
	}
	var swept int
	for _, e := range entries {
		p := filepath.Join(scratch, e.Name())
		if err := os.RemoveAll(p); err != nil {
			s.log.Warn("failed to remove scratch leftover", "path", p, "error", err)
			continue
		}
		swept++
	}
	if swept > 0 {
		s.log.Info("swept scratch leftovers", "count", swept)
	}
}

// Start queues a download and returns the jobID immediately. The
// actual pipeline runs in a goroutine and publishes progress on
// the channel returned by Subscribe(jobID).
//
// Returns ErrBusy if there's already an active download for this
// broadcaster — prevents two copies of the same stream running
// in parallel.
func (s *Service) Start(ctx context.Context, p Params) (string, error) {
	s.mu.Lock()
	for _, existing := range s.active {
		if existing.broadcasterID == p.BroadcasterID {
			s.mu.Unlock()
			return "", ErrBusy
		}
	}
	maxConcurrent := s.cfg.App.Download.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 2
	}
	if len(s.active) >= maxConcurrent {
		s.mu.Unlock()
		return "", fmt.Errorf("downloader: at max concurrent downloads (%d)", maxConcurrent)
	}

	jobID := uuid.NewString()
	filename := buildFilename(p.BroadcasterLogin, jobID)

	d := &download{
		jobID:         jobID,
		broadcasterID: p.BroadcasterID,
		progressCh:    make(chan Progress, 16),
		startedAt:     time.Now(),
	}
	s.active[jobID] = d
	s.mu.Unlock()

	vid, err := s.repo.CreateVideo(ctx, &repository.VideoInput{
		JobID:         jobID,
		Filename:      filename,
		DisplayName:   p.DisplayName,
		Status:        repository.VideoStatusPending,
		Quality:       p.Quality,
		BroadcasterID: p.BroadcasterID,
		StreamID:      p.StreamID,
		ViewerCount:   p.ViewerCount,
		Language:      p.Language,
		RecordingType: p.RecordingType,
		ForceH264:     p.ForceH264,
	})
	if err != nil {
		s.mu.Lock()
		delete(s.active, jobID)
		s.mu.Unlock()
		return "", fmt.Errorf("create video row: %w", err)
	}
	d.videoID = vid.ID

	runCtx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel

	go s.run(runCtx, d, p, filename)
	return jobID, nil
}

// Cancel asks the in-flight pipeline to stop. userCancelled is set
// first so the run goroutine's failure handler records ErrCancelled
// rather than "context canceled."
//
// No-op if the jobID isn't active.
func (s *Service) Cancel(jobID string) {
	s.mu.Lock()
	d, ok := s.active[jobID]
	if ok {
		d.userCancelled = true
	}
	s.mu.Unlock()
	if !ok || d.cancel == nil {
		return
	}
	d.cancel()
}

// Subscribe returns the progress channel for a running job. Nil
// when the job has completed or was never started.
func (s *Service) Subscribe(jobID string) <-chan Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.active[jobID]; ok {
		return d.progressCh
	}
	return nil
}

// Shutdown cancels all active downloads. Called from the server's
// graceful-shutdown path.
func (s *Service) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.active {
		d.cancel()
	}
}

// ErrBusy is returned by Start when a download for the broadcaster
// is already in flight. Callers that want to replace the running
// download should call Cancel first.
//
// ErrCancelled marks a download that was terminated by a user
// Cancel() rather than crashing. Distinguishing matters for the UI.
var (
	ErrBusy      = errors.New("downloader: broadcaster already has an active download")
	ErrCancelled = errors.New("downloader: cancelled by user")
)

// run walks the full pipeline for one job. All DB writes use
// dbCtx (derived from context.WithoutCancel) instead of the
// runtime ctx so a user Cancel() still lets the "mark failed"
// write land.
func (s *Service) run(ctx context.Context, d *download, p Params, filename string) {
	log := s.log.With("job_id", d.jobID, "broadcaster_login", p.BroadcasterLogin)
	dbCtx := context.WithoutCancel(ctx)

	defer func() {
		close(d.progressCh)
		s.mu.Lock()
		delete(s.active, d.jobID)
		s.mu.Unlock()
	}()

	if err := s.repo.UpdateVideoStatus(dbCtx, d.videoID, repository.VideoStatusRunning); err != nil {
		log.Error("failed to mark video running", "error", err)
	}

	// Per-job scratch layout:
	//   <scratch>/<jobID>/segments/   — .ts / .m4s fragments + init.mp4
	//   <scratch>/<jobID>/<base>.mp4  — remuxed output
	//   <scratch>/<jobID>/<base>.jpg  — thumbnail
	// One RemoveAll at the end gets everything.
	jobDir := filepath.Join(s.cfg.Env.ScratchDir, d.jobID)
	segmentsDir := filepath.Join(jobDir, "segments")
	if err := os.MkdirAll(segmentsDir, 0o755); err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("create scratch dir: %w", err))
		return
	}
	defer func() { _ = os.RemoveAll(jobDir) }()

	// Stage 1: auth — anonymous playback for Phase 6a. OAuth-token
	// path (ad-skip + HEVC unlock) is Phase 6c.
	s.emitProgress(d, "auth")
	token, err := s.twitch.PlaybackToken(ctx, p.BroadcasterLogin, "")
	if err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("playback token: %w", err))
		return
	}

	// Stages 2 + 3: master playlist + variant selection. Gap policy
	// fields drive Stage 4 and Stage 3 alike, so build SelectOptions
	// once and reuse.
	s.emitProgress(d, "playlist")
	selectOpts := twitch.SelectOptions{
		RecordingType: p.RecordingType,
		Quality:       qualityToHeight(p.Quality),
		EnableAV1:     s.cfg.App.Download.EnableAV1,
		DisableHEVC:   s.cfg.App.Download.DisableHEVC,
		ForceH264:     p.ForceH264,
	}
	if selectOpts.RecordingType == "" {
		selectOpts.RecordingType = twitch.RecordingTypeVideo
	}
	manifest, err := s.twitch.FetchMasterPlaylist(ctx, p.BroadcasterLogin, token, selectOpts)
	if err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("master playlist: %w", err))
		return
	}
	variant, err := twitch.SelectVariant(manifest, selectOpts)
	if err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("variant selection: %w", err))
		return
	}
	log.Info("selected variant", "quality", variant.Quality, "codec", variant.Codec)

	// Stage 4: HLS fetch — segments stream into segmentsDir. Progress
	// is published by the orchestrator through its own channel;
	// we bridge it to downloader.Progress via a separate goroutine.
	hlsProgress := make(chan hls.Progress, 32)
	go s.bridgeProgress(d, hlsProgress)
	s.emitProgress(d, "segments")
	hlsResult, err := hls.Run(ctx, hls.JobConfig{
		MediaPlaylistURL:   variant.URL,
		WorkDir:            segmentsDir,
		Fetcher:            s.fetcher,
		SegmentConcurrency: s.cfg.App.Download.SegmentConcurrency,
		Log:                log,
		Progress:           hlsProgress,
		GapPolicy: hls.GapPolicy{
			Strict:      s.cfg.App.Download.Strict,
			MaxGapRatio: s.cfg.App.Download.MaxGapRatio,
		},
	})
	if err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("hls run: %w", err))
		return
	}

	// Stage 5 + 6: remux. Pick the ffmpeg mode from what the hls
	// orchestrator observed — the media-playlist capability gate
	// is what actually decided ts vs fmp4.
	s.emitProgress(d, "remux")
	remuxMode := remux.ModeTS
	if hlsResult.Kind == hls.SegmentKindFMP4 {
		remuxMode = remux.ModeFMP4
	}
	kind := kindFromRecordingType(selectOpts.RecordingType)
	inputPath, err := remux.PrepareInput(segmentsDir, remuxMode)
	if err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("remux prep: %w", err))
		return
	}
	remuxIn := remux.RunInput{
		Mode:           remuxMode,
		Kind:           kind,
		InputPath:      inputPath,
		OutputDir:      jobDir,
		OutputBasename: filename,
	}
	if err := s.remuxer.Run(ctx, remuxIn); err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("remux: %w", err))
		return
	}
	remuxedPath := remuxIn.OutputPath()

	// Stage 7: probe.
	s.emitProgress(d, "metadata")
	probeResult, err := s.probe.Run(ctx, remuxedPath)
	if err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("probe: %w", err))
		return
	}

	// Stage 9: corruption check + heal. If duration mismatch is
	// within tolerance we skip entirely. On heal failure we keep
	// the un-healed file per spec ("partial VOD is better than
	// none").
	if isCorrupt(probeResult, kind) {
		log.Info("duration mismatch — running heal pass",
			"format_duration", probeResult.Duration,
			"threshold", remux.CorruptionThreshold)
		healedPath := filepath.Join(jobDir, filename+".healed"+kind.OutputExt())
		if err := s.remuxer.Heal(ctx, remuxedPath, healedPath, kind); err != nil {
			log.Warn("heal failed; keeping un-healed file", "error", err)
		} else if healedResult, probeErr := s.probe.Run(ctx, healedPath); probeErr != nil {
			log.Warn("re-probe of healed file failed; keeping un-healed", "error", probeErr)
			_ = os.Remove(healedPath)
		} else if isCorrupt(healedResult, kind) {
			log.Warn("heal did not resolve corruption; keeping un-healed")
			_ = os.Remove(healedPath)
		} else {
			if err := os.Rename(healedPath, remuxedPath); err != nil {
				log.Warn("heal-rename failed; keeping un-healed", "error", err)
				_ = os.Remove(healedPath)
			} else {
				probeResult = healedResult
			}
		}
	}

	// Stage 8: thumbnail. Audio jobs skip entirely; the UI falls
	// back to the channel avatar.
	var thumbRel string
	if kind == remux.KindVideo {
		s.emitProgress(d, "thumbnail")
		thumbPath := filepath.Join(jobDir, filename+".jpg")
		err := s.thumb.Generate(ctx, thumbnail.Input{
			VideoPath:       remuxedPath,
			OutputPath:      thumbPath,
			DurationSeconds: probeResult.Duration,
		})
		switch {
		case err == nil:
			thumbRel = filepath.ToSlash(filepath.Join("thumbnails", filename+".jpg"))
		case errors.Is(err, thumbnail.ErrAllTriesSingleColor):
			log.Info("thumbnail: all frames monochrome; leaving unset")
		default:
			log.Warn("thumbnail generation failed; continuing without thumbnail", "error", err)
		}
	}

	// Stage 10: store. Video first, then thumbnail — if the
	// thumbnail upload fails we still want the video playable.
	videoRel := filepath.ToSlash(filepath.Join("videos", filename+kind.OutputExt()))
	if err := s.uploadFromScratch(ctx, remuxedPath, videoRel); err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("upload video: %w", err))
		return
	}
	var thumbPtr *string
	if thumbRel != "" {
		thumbPath := filepath.Join(jobDir, filename+".jpg")
		if err := s.uploadFromScratch(ctx, thumbPath, thumbRel); err != nil {
			log.Warn("thumbnail upload failed; continuing without thumbnail", "error", err)
		} else {
			thumbPtr = &thumbRel
		}
	}

	// Stage 11: mark done. The deferred RemoveAll handles the
	// scratch cleanup.
	if err := s.repo.MarkVideoDone(dbCtx, d.videoID, probeResult.Duration, probeResult.Size, thumbPtr); err != nil {
		log.Error("failed to mark video done", "error", err)
		return
	}
	s.emitProgress(d, "done")
	log.Info("download complete",
		"duration_seconds", probeResult.Duration,
		"size_bytes", probeResult.Size,
		"gaps", hlsResult.SegmentsGaps,
		"segments", hlsResult.SegmentsDone,
	)
}

// kindFromRecordingType maps the spec's recording_type enum to
// remux.Kind. Empty or unknown values fall through to video,
// matching the repo CHECK constraint's default.
func kindFromRecordingType(rt string) remux.Kind {
	if rt == twitch.RecordingTypeAudio {
		return remux.KindAudio
	}
	return remux.KindVideo
}

// isCorrupt applies the spec's Stage 9 duration-mismatch rule.
// Zero durations on either side are treated as "can't measure,
// don't heal" — probe.parseProbeOutput returns zero on "N/A"
// values and we'd rather skip healing than trigger it on noise.
func isCorrupt(r *probe.Result, kind remux.Kind) bool {
	if r == nil || r.Duration == 0 {
		return false
	}
	var streamDur float64
	switch kind {
	case remux.KindAudio:
		if r.AudioStream != nil {
			streamDur = r.AudioStream.Duration
		}
	default:
		if r.VideoStream != nil {
			streamDur = r.VideoStream.Duration
		}
	}
	if streamDur == 0 {
		return false
	}
	return math.Abs(r.Duration-streamDur) > remux.CorruptionThreshold
}

// bridgeProgress translates hls.Progress events into the
// downloader.Progress shape. Runs until the hls channel is
// closed — which the hls orchestrator does on every termination
// path — and then exits.
//
// Phase 6a only forwards the stage + segment count as a textual
// label; exact Percent/Speed/ETA math against a live stream
// (SegmentsTotal unknown until EXT-X-ENDLIST) is deferred.
func (s *Service) bridgeProgress(d *download, in <-chan hls.Progress) {
	for hp := range in {
		prog := Progress{
			JobID:   d.jobID,
			Stage:   "segments",
			Percent: -1,
		}
		_ = hp // reserved for richer fields in a later phase
		select {
		case d.progressCh <- prog:
		default:
			// Buffered channel is full; progress is
			// informational — drop rather than block the
			// pipeline.
		}
	}
}

// emitProgress sends a stage-transition event best-effort. Drops
// the event when the buffered channel is full (slow subscriber
// or none at all); the final "done" event goes through the same
// path and subscribers rely on Subscribe returning nil to know
// the job has ended.
func (s *Service) emitProgress(d *download, stage string) {
	prog := Progress{JobID: d.jobID, Stage: stage, Percent: -1}
	select {
	case d.progressCh <- prog:
	default:
	}
}

// uploadFromScratch opens a scratch file and streams it to the
// Storage backend at the given relative path. For local storage
// this is an atomic move; for S3 it uploads bytes.
func (s *Service) uploadFromScratch(ctx context.Context, scratchPath, storagePath string) error {
	f, err := os.Open(scratchPath)
	if err != nil {
		return fmt.Errorf("open scratch: %w", err)
	}
	defer f.Close()
	if err := s.storage.Save(ctx, filepath.ToSlash(storagePath), f); err != nil {
		return fmt.Errorf("save to storage: %w", err)
	}
	return nil
}

// failDownload records a failure on the video row. If the
// download was cancelled by a user call to Cancel(), the
// recorded error is ErrCancelled so the UI can distinguish
// "admin stopped this" from a real crash.
func (s *Service) failDownload(dbCtx context.Context, d *download, log *slog.Logger, cause error) {
	s.mu.Lock()
	userCancelled := d.userCancelled
	s.mu.Unlock()

	recorded := cause
	if userCancelled {
		recorded = ErrCancelled
		log.Info("download cancelled by user")
	} else {
		log.Error("download failed", "error", cause)
	}
	if err := s.repo.MarkVideoFailed(dbCtx, d.videoID, recorded.Error()); err != nil {
		log.Error("failed to mark video failed", "error", err)
	}
}

// buildFilename generates a deterministic, filesystem-safe
// filename tied to the job ID so a retry of the same broadcaster
// doesn't clobber the original. Format:
// <UTC timestamp>-<login>-<short jobID>.
func buildFilename(login, jobID string) string {
	ts := time.Now().UTC().Format("20060102-150405")
	short := strings.ReplaceAll(jobID, "-", "")
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf("%s-%s-%s", ts, login, short)
}
