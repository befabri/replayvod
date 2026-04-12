// Package downloader runs the yt-dlp → ffmpeg pipeline that turns a live
// Twitch stream into a stored MP4. Each download has a unique jobID so
// callers (tRPC handlers, the scheduler, webhook handlers) can subscribe
// to progress updates and request cancellation without holding a reference
// to the *exec.Cmd.
package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
)

// Resolution preferences used for fallback. Order inside each list is
// preference when the requested resolution isn't available.
var fallbackResolutions = map[string][]string{
	"1080": {"720", "480", "360"},
	"720":  {"480", "360"},
	"480":  {"360", "160"},
	"360":  {"160"},
	"160":  {},
}

// qualityToHeight maps the domain-level quality enum to yt-dlp height filters.
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

// Params describes a single download request.
type Params struct {
	BroadcasterID    string  // Twitch broadcaster ID; used for Video row.
	BroadcasterLogin string  // Twitch login; used to build the Twitch URL.
	DisplayName      string  // Broadcaster display name; stored on Video.
	Quality          string  // repository.Quality* constant.
	Language         string  // Stream language at download start.
	ViewerCount      int64   // Viewer count at download start.
	StreamID         *string // Stream this download came from, if known.
}

// Progress is a single snapshot written to the per-job progress channel.
// Downloads emit one Progress per yt-dlp stderr line we can parse.
type Progress struct {
	JobID   string
	Stage   string  // "yt-dlp", "ffmpeg", "metadata"
	Percent float64 // 0–100; -1 if not yet known
	Speed   string  // Raw yt-dlp speed field, e.g. "2.4MiB/s"
	ETA     string  // Raw yt-dlp ETA field, e.g. "00:42"
}

// Service orchestrates downloads. Safe for concurrent use.
type Service struct {
	cfg     *config.Config
	repo    repository.Repository
	storage storage.Storage
	log     *slog.Logger

	mu     sync.Mutex
	active map[string]*download // keyed by jobID
}

// download is the per-job state kept in memory. The exec.Cmd is tracked so
// Cancel() can send SIGTERM via cmd.Cancel, and progress is published through
// progressCh. broadcasterID is tracked for dedup on Start.
type download struct {
	jobID         string
	videoID       int64
	broadcasterID string
	cancel        context.CancelFunc
	userCancelled bool // set by Cancel() to distinguish from error-driven cancels
	progressCh    chan Progress
	startedAt     time.Time
}

// NewService creates a new downloader. Sweeps any orphaned .tmp.mp4 files
// from the videos/ directory on startup — these are leftovers from a crash
// or SIGKILL'd yt-dlp that never got to finish. Failing that sweep is
// non-fatal; we log and move on.
func NewService(cfg *config.Config, repo repository.Repository, store storage.Storage, log *slog.Logger) *Service {
	s := &Service{
		cfg:     cfg,
		repo:    repo,
		storage: store,
		log:     log.With("domain", "downloader"),
		active:  make(map[string]*download),
	}
	s.sweepOrphanedTemps()
	return s
}

// sweepOrphanedTemps removes leftover files from the scratch dir.
// Called once at startup — partial yt-dlp output that survived a crash
// or hard kill is never resumable, so cleanup is always safe. The
// scratch dir is local by definition (subprocesses can't write to S3
// directly), so this works uniformly across storage backends.
func (s *Service) sweepOrphanedTemps() {
	scratch := s.cfg.Env.ScratchDir
	entries, err := os.ReadDir(scratch)
	if err != nil {
		// Missing dir is expected on first boot; other errors shouldn't
		// stop us from starting the server.
		return
	}
	var swept int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(scratch, e.Name())
		if err := os.Remove(p); err != nil {
			s.log.Warn("failed to remove scratch leftover", "path", p, "error", err)
			continue
		}
		swept++
	}
	if swept > 0 {
		s.log.Info("swept scratch leftovers", "count", swept)
	}
}

// Start queues a download and returns the jobID immediately. The actual
// yt-dlp/ffmpeg work runs in a goroutine and publishes progress on the
// channel returned by Subscribe(jobID).
//
// Returns ErrBusy if there's already an active download for this broadcaster —
// prevents accidentally running two copies of the same stream.
func (s *Service) Start(ctx context.Context, p Params) (string, error) {
	s.mu.Lock()
	// Dedup in the same critical section as the insert below so two concurrent
	// calls for the same broadcaster can't both pass the check.
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
	// Register before the DB write so a concurrent Start sees us. If the
	// CreateVideo fails we'll un-register.
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

// Cancel asks the in-flight pipeline to stop. The exec.Cmd.Cancel hook sends
// SIGTERM; if the subprocess doesn't honor it within cmd.WaitDelay, the stdlib
// falls back to SIGKILL. userCancelled is set first so the run goroutine's
// failure handler records ErrCancelled instead of "context canceled".
//
// No-op if the jobID isn't active (already done, never started, or already
// cancelled).
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

// Subscribe returns the progress channel for a running job.
// Returns nil if the job has completed or was never started.
func (s *Service) Subscribe(jobID string) <-chan Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.active[jobID]; ok {
		return d.progressCh
	}
	return nil
}

// Shutdown cancels all active downloads. Called from the server's graceful
// shutdown path.
func (s *Service) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.active {
		d.cancel()
	}
}

// ErrBusy is returned by Start when a download for the broadcaster is already
// in flight. Callers that want to replace the running download should call
// Cancel first.
//
// ErrCancelled marks a download that was terminated by a user Cancel() rather
// than crashing. Distinguishing these matters for the UI — admins want to
// know "you stopped this" vs "something broke".
var (
	ErrBusy      = errors.New("downloader: broadcaster already has an active download")
	ErrCancelled = errors.New("downloader: cancelled by user")
)

// run is the full pipeline: select resolution → yt-dlp download →
// ffmpeg remux → probe metadata → update DB. Runs in a goroutine.
//
// All DB writes inside run() use dbCtx (derived from context.WithoutCancel)
// instead of the runtime ctx. When a user calls Cancel() the runtime ctx is
// cancelled immediately, but we still want the "mark failed" write to land —
// otherwise the UI sees a stuck RUNNING row forever.
func (s *Service) run(ctx context.Context, d *download, p Params, filename string) {
	log := s.log.With("job_id", d.jobID, "broadcaster_login", p.BroadcasterLogin)
	dbCtx := context.WithoutCancel(ctx)

	defer func() {
		close(d.progressCh)
		s.mu.Lock()
		delete(s.active, d.jobID)
		s.mu.Unlock()
	}()

	// Mark as RUNNING.
	if err := s.repo.UpdateVideoStatus(dbCtx, d.videoID, repository.VideoStatusRunning); err != nil {
		log.Error("failed to mark video running", "error", err)
	}

	// Subprocess IO always lands in ScratchDir first: yt-dlp and
	// ffmpeg can't write to an S3 bucket. After the pipeline
	// finishes, Save() uploads to whichever Storage backend is
	// configured. Local backend is just a move at Save time.
	//
	// Layout is flat: videos/<filename>.mp4. The filename already embeds
	// the broadcaster login + timestamp, so no nested subdirs needed.
	scratch := s.cfg.Env.ScratchDir
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("create scratch dir: %w", err))
		return
	}
	tmpPath := filepath.Join(scratch, filename+".tmp.mp4")
	scratchFinal := filepath.Join(scratch, filename+".mp4")
	videoRel := filepath.Join("videos", filename+".mp4")

	// Ensure scratch leftovers from this jobID are cleaned up on every
	// exit path. The orphan sweep handles crashes; this handles
	// normal completion + failures.
	defer func() {
		_ = os.Remove(tmpPath)
		_ = os.Remove(scratchFinal)
	}()

	// Resolution selection.
	preferred := qualityToHeight(p.Quality)
	url := "https://www.twitch.tv/" + p.BroadcasterLogin
	resolution, err := s.selectResolution(ctx, url, preferred)
	if err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("select resolution: %w", err))
		return
	}
	log.Info("starting download", "resolution", resolution, "url", url)

	// Stage 1: yt-dlp.
	if err := s.runYtDlp(ctx, d, url, resolution, tmpPath); err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("yt-dlp: %w", err))
		return
	}

	// Stage 2: ffmpeg remux with audio rate fix.
	if err := s.runFFmpeg(ctx, d, tmpPath, scratchFinal); err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("ffmpeg: %w", err))
		return
	}
	_ = os.Remove(tmpPath)

	// Stage 3: metadata extraction.
	d.progressCh <- Progress{JobID: d.jobID, Stage: "metadata", Percent: 0}
	duration, err := probeDuration(ctx, scratchFinal)
	if err != nil {
		log.Warn("probe duration failed; continuing", "error", err)
	}
	info, err := os.Stat(scratchFinal)
	if err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("stat final: %w", err))
		return
	}

	// Stage 4: thumbnail. ffmpeg writes to a scratch path; we upload
	// after the video so the Video row never references a thumbnail
	// that doesn't yet exist in storage.
	scratchThumb := filepath.Join(scratch, filename+".jpg")
	thumbRel := filepath.Join("thumbnails", filename+".jpg")
	defer os.Remove(scratchThumb)
	if err := s.generateThumbnail(ctx, scratchFinal, scratchThumb, duration); err != nil {
		log.Warn("thumbnail generation failed; continuing without thumbnail", "error", err)
		thumbRel = ""
	}

	// Stage 5: upload to Storage. Video first, then thumbnail — if
	// thumbnail upload fails we still want the video playable.
	if err := s.uploadFromScratch(ctx, scratchFinal, videoRel); err != nil {
		s.failDownload(dbCtx, d, log, fmt.Errorf("upload video: %w", err))
		return
	}
	var thumbPtr *string
	if thumbRel != "" {
		if err := s.uploadFromScratch(ctx, scratchThumb, thumbRel); err != nil {
			log.Warn("thumbnail upload failed; continuing without thumbnail", "error", err)
		} else {
			thumbPtr = &thumbRel
		}
	}

	if err := s.repo.MarkVideoDone(dbCtx, d.videoID, duration, info.Size(), thumbPtr); err != nil {
		log.Error("failed to mark video done", "error", err)
		return
	}
	log.Info("download complete", "duration_seconds", duration, "size_bytes", info.Size())
}

// uploadFromScratch opens a scratch file and streams it to the Storage
// backend at the given relative path. For local storage this is an
// atomic move under the hood; for S3 it uploads bytes. Always uses
// forward slashes for the remote path.
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

// failDownload records a failure on the video row. If the download was
// cancelled by a user call to Cancel(), the recorded error is ErrCancelled
// so the UI can distinguish "admin stopped this" from a real crash.
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

// selectResolution queries yt-dlp for available formats and picks the
// preferred height, falling back through fallbackResolutions if unavailable.
func (s *Service) selectResolution(ctx context.Context, url, preferred string) (string, error) {
	available, err := s.listResolutions(ctx, url)
	if err != nil {
		return "", err
	}
	if contains(available, preferred) {
		return preferred, nil
	}
	for _, candidate := range fallbackResolutions[preferred] {
		if contains(available, candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no suitable resolution from %v (wanted %s)", available, preferred)
}

// listResolutions runs `yt-dlp --list-formats` and extracts the unique
// heights mentioned (e.g., "720p", "1080p") from stdout.
func (s *Service) listResolutions(ctx context.Context, url string) ([]string, error) {
	cmd := exec.CommandContext(ctx, s.cfg.Env.YtdlpPath, "--list-formats", url)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list formats: %w (output: %s)", err, string(out))
	}
	re := regexp.MustCompile(`(\d{3,4})p`)
	seen := make(map[string]bool)
	var results []string
	for _, m := range re.FindAllStringSubmatch(string(out), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			results = append(results, m[1])
		}
	}
	return results, nil
}

// runYtDlp runs yt-dlp and parses stderr for progress updates. yt-dlp emits
// lines like `[download]  12.3% of ~4.56GiB at 2.40MiB/s ETA 00:42`.
func (s *Service) runYtDlp(ctx context.Context, d *download, url, resolution, out string) error {
	cmd := exec.CommandContext(ctx, s.cfg.Env.YtdlpPath,
		url,
		"--format", "best[height="+resolution+"]",
		"--output", out,
		"--fixup", "never",
		"--newline", // forces progress updates on new lines for easier parsing
	)
	gracefulTerminate(cmd)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	// Progress parsing pulls from stdout (yt-dlp writes progress there with
	// --newline). Errors come from stderr and get logged.
	go s.parseProgress(d, stdout, "yt-dlp")
	go func() {
		b, _ := io.ReadAll(stderr)
		if len(b) > 0 {
			s.log.Debug("yt-dlp stderr", "job_id", d.jobID, "output", string(b))
		}
	}()

	return cmd.Wait()
}

// runFFmpeg remuxes the yt-dlp output with an audio rate fix and copies the
// video stream so there's no re-encoding overhead.
func (s *Service) runFFmpeg(ctx context.Context, d *download, in, out string) error {
	rate := s.cfg.App.Download.AudioRate
	if rate <= 0 {
		rate = 48000
	}
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-i", in,
		"-c:v", "copy",
		"-af", fmt.Sprintf("asetrate=%d", rate),
		out,
	)
	gracefulTerminate(cmd)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	// ffmpeg emits progress on stderr. We don't parse it precisely — the
	// phase is short relative to the download and a single "ffmpeg stage
	// in progress" signal is enough for the UI.
	d.progressCh <- Progress{JobID: d.jobID, Stage: "ffmpeg", Percent: -1}
	go func() {
		io.Copy(io.Discard, stderr) //nolint:errcheck
	}()

	return cmd.Wait()
}

// parseProgress scans yt-dlp stdout for `[download]` lines and publishes
// Progress events to the channel.
func (s *Service) parseProgress(d *download, r io.Reader, stage string) {
	reDownload := regexp.MustCompile(`\[download\]\s+(\d+(?:\.\d+)?)%.*?at\s+([^\s]+)(?:\s+ETA\s+([^\s]+))?`)
	buf := make([]byte, 4096)
	var acc strings.Builder
	for {
		n, err := r.Read(buf)
		if n > 0 {
			acc.Write(buf[:n])
			for {
				s := acc.String()
				idx := strings.IndexByte(s, '\n')
				if idx < 0 {
					// Also handle yt-dlp's \r updates
					idx = strings.IndexByte(s, '\r')
					if idx < 0 {
						break
					}
				}
				line := s[:idx]
				acc.Reset()
				acc.WriteString(s[idx+1:])

				if m := reDownload.FindStringSubmatch(line); m != nil {
					pct, _ := strconv.ParseFloat(m[1], 64)
					prog := Progress{JobID: d.jobID, Stage: stage, Percent: pct, Speed: m[2]}
					if len(m) > 3 {
						prog.ETA = m[3]
					}
					select {
					case d.progressCh <- prog:
					default:
						// Channel is buffered at 16 — drop updates if a slow
						// subscriber can't keep up. Progress is informational.
					}
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// generateThumbnail runs ffmpeg to capture a frame at ~10% of the video
// duration and writes it to outPath (a local filesystem path — the
// caller uploads it to storage afterward). Picks a frame at 10% of the
// video, clamped to [5s, 600s] so very short clips still get a frame
// and long streams don't bury the thumbnail late.
func (s *Service) generateThumbnail(ctx context.Context, videoPath, outPath string, durationSec float64) error {
	offset := durationSec * 0.1
	if offset < 5 {
		offset = 5
	}
	if offset > 600 {
		offset = 600
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-ss", fmt.Sprintf("%.2f", offset),
		"-i", videoPath,
		"-vframes", "1",
		"-q:v", "3",
		outPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg thumbnail: %w (output: %s)", err, string(out))
	}
	return nil
}

// probeDuration uses ffprobe to read the container duration. Returns 0 if
// ffprobe is missing or the output can't be parsed — missing duration
// doesn't fail the whole download.
func probeDuration(ctx context.Context, path string) (float64, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}

// buildFilename generates a deterministic, filesystem-safe filename tied to
// the job ID so a retry of the same broadcaster doesn't clobber the original.
func buildFilename(login, jobID string) string {
	ts := time.Now().UTC().Format("20060102-150405")
	// Short UUID suffix is enough — jobID is already unique, but the
	// timestamp prefix makes files sortable in a directory listing.
	short := strings.ReplaceAll(jobID, "-", "")
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf("%s-%s-%s", ts, login, short)
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// gracefulTerminate configures cmd so that context cancellation sends
// SIGTERM (give the subprocess a chance to clean up) with a 5s grace period
// before the stdlib fallback kills it with SIGKILL.
//
// Only applies on POSIX — on Windows, Process.Signal doesn't support SIGTERM,
// so we fall back to the default Kill behavior. Acceptable because the
// downloader is primarily a Linux homelab target.
//
// ffmpeg honors SIGTERM cleanly. yt-dlp historically ignores SIGTERM in
// favor of its own handlers; the WaitDelay ensures we escalate to SIGKILL
// rather than hanging. The partial temp file is cleaned up by the startup
// sweep (see LocalStorage init path).
func gracefulTerminate(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second
}
