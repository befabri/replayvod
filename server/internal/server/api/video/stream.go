package video

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/befabri/replayvod/server/internal/mediastore"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/videodownload"
	"github.com/befabri/replayvod/server/internal/waveform"
	"github.com/go-chi/chi/v5"
)

// statusClientClosed is nginx's non-standard 499. Browsers abort Range requests
// on seeks; metadata reads cancelled by those aborts should not report server errors.
const statusClientClosed = 499

func clientGone(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// PlaybackBuilder admits work independently of the HTTP request and must
// coalesce concurrent requests for the same recording.
type PlaybackBuilder interface {
	StartBuild(ctx context.Context, videoID int64) error
}

// MissingMarker reconciles definitive missing-media reads. A nil marker
// leaves the recording unchanged and the request returns 404.
type MissingMarker interface {
	MarkMissing(ctx context.Context, videoID int64) (bool, error)
}

// StorageGate provides cached readiness for ordinary reads and a fresh probe
// before artifact publication or destructive reconciliation. Nil means always;
// read-only storage still serves reads.
type StorageGate interface {
	Ready() error
	Verify(context.Context) error
}

const (
	markMissingTimeout     = 3 * time.Second
	markMissingCooldown    = time.Minute
	maxMissingChecks       = 128
	maxActiveMissingChecks = 8
)

type missingCheck struct {
	done      chan struct{}
	checkedAt time.Time
	err       error
}

type StreamHandler struct {
	repo     repository.Repository
	storage  *mediastore.Store
	verifier *videodownload.Verifier
	builder  PlaybackBuilder
	missing  MissingMarker
	log      *slog.Logger
	// missingMu protects both active checks and the bounded success cache.
	missingMu           sync.Mutex
	missingChecks       map[int64]*missingCheck
	activeMissingChecks int
	waveformFlights     *waveformFlights
	waveformGenerator   waveform.Generator
}

type StreamHandlerOption func(*StreamHandler)

func NewStreamHandler(repo repository.Repository, store *mediastore.Store, verifier *videodownload.Verifier, log *slog.Logger, opts ...StreamHandlerOption) *StreamHandler {
	h := &StreamHandler{
		repo:              repo,
		storage:           store,
		verifier:          verifier,
		log:               log.With("domain", "video-stream"),
		waveformFlights:   newWaveformFlights(),
		waveformGenerator: waveform.FFmpegGenerator{},
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func WithPlaybackBuilder(b PlaybackBuilder) StreamHandlerOption {
	return func(h *StreamHandler) { h.builder = b }
}

func WithMissingMarker(m MissingMarker) StreamHandlerOption {
	return func(h *StreamHandler) { h.missing = m }
}

func WithWaveformGenerator(g WaveformGenerator) StreamHandlerOption {
	return func(h *StreamHandler) {
		if g != nil {
			h.waveformGenerator = g
		}
	}
}

// SetupRoutes registers session-authenticated media routes. Pass the same
// session middleware used by the tRPC routes.
func (h *StreamHandler) SetupRoutes(r chi.Router, authMiddleware func(http.Handler) http.Handler) {
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware)
		r.Get("/videos/{id}/playback/stream", h.streamPlayback)
		r.Head("/videos/{id}/playback/stream", h.streamPlayback)
		r.Get("/videos/{id}/parts/{part}/stream", h.streamPart)
		r.Head("/videos/{id}/parts/{part}/stream", h.streamPart)
		r.Get("/videos/{id}/waveform", h.streamAudioWaveform)
		r.Get("/thumbnails/*", h.serveThumbnail)
	})
}

// SetupSignedRoutes must be registered outside session middleware: the URL
// signature and expiry authorize webhook consumers without cookies. HEAD
// probes use the same authorization and part resolution as GET.
func (h *StreamHandler) SetupSignedRoutes(r chi.Router) {
	r.Get("/videos/{id}/parts/{part}/download", h.streamSignedPart)
	r.Head("/videos/{id}/parts/{part}/download", h.streamSignedPart)
}

func (h *StreamHandler) streamPlayback(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid video id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	video, err := h.repo.GetVideo(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if clientGone(err) {
			http.Error(w, "client closed request", statusClientClosed)
			return
		}
		h.log.Error("playback stream: get video failed", "error", err, "id", id)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if video.Status != repository.VideoStatusDone {
		http.Error(w, "video not available", http.StatusNotFound)
		return
	}
	if video.DeletedAt != nil {
		http.Error(w, "video deleted", http.StatusGone)
		return
	}

	asset, err := h.repo.GetVideoPlaybackAsset(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if clientGone(err) {
			http.Error(w, "client closed request", statusClientClosed)
			return
		}
		h.log.Error("playback stream: get asset failed", "error", err, "id", id)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if asset.Status != repository.PlaybackAssetStatusReady || asset.Filename == nil {
		http.NotFound(w, r)
		return
	}
	relPath := storagekeys.Video(*asset.Filename)
	// Storage that is not attached says nothing about the artifact: answer as an
	// outage and keep the ready row.
	if err := h.storageUnavailable(); err != nil {
		h.log.Warn("playback artifact requested while storage is not attached", "video_id", id, "error", err)
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	// Only a definitive missing file can demote a ready artifact to part playback.
	info, statErr := h.storage.Stat(ctx, relPath)
	switch {
	case clientGone(statErr) || ctx.Err() != nil:
		http.Error(w, "client closed request", statusClientClosed)
		return
	case errors.Is(statErr, fs.ErrNotExist):
		var status int
		asset, info, status = h.recheckMissingPlayback(ctx, id)
		if status != http.StatusOK {
			message := http.StatusText(status)
			if status == statusClientClosed {
				message = "client closed request"
			}
			http.Error(w, message, status)
			return
		}
		relPath = storagekeys.Video(*asset.Filename)
	case statErr != nil:
		h.serveStorageFile(w, r, id, relPath, *asset.Filename)
		return
	}
	if r.Method == http.MethodGet && isPlaybackSessionStart(r) {
		// Touching every Range chunk would serialize SQLite writes against recording.
		if err := h.repo.TouchVideoPlaybackAsset(ctx, id); err != nil {
			h.log.Warn("touch playback asset failed", "video_id", id, "error", err)
		}
	}
	// The stored MIME type is authoritative; reuse the Stat result below.
	if asset.MimeType != nil && *asset.MimeType != "" {
		w.Header().Set("Content-Type", *asset.MimeType)
	}
	h.serveStorageFileInfo(w, r, id, relPath, *asset.Filename, info)
}

// recheckMissingPlayback owns only stale-row reconciliation. A rebuild may
// have published a different row or filename since the first missing Stat, so
// inspect the current row and file under the same lock as publication/deletion.
// Release ownership before streaming or calling the missing-recording marker,
// whose reconciliation can acquire this same recording lock.
func (h *StreamHandler) recheckMissingPlayback(ctx context.Context, id int64) (*repository.VideoPlaybackAsset, storage.FileInfo, int) {
	unlock, err := h.storage.Lock(ctx, id)
	if err != nil {
		return nil, storage.FileInfo{}, statusClientClosed
	}
	defer unlock.Close()
	asset, err := h.repo.GetVideoPlaybackAsset(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, storage.FileInfo{}, http.StatusNotFound
		}
		if clientGone(err) {
			return nil, storage.FileInfo{}, statusClientClosed
		}
		h.log.Error("playback stream: recheck asset failed", "error", err, "id", id)
		return nil, storage.FileInfo{}, http.StatusInternalServerError
	}
	if asset.Status != repository.PlaybackAssetStatusReady || asset.Filename == nil {
		return nil, storage.FileInfo{}, http.StatusNotFound
	}
	info, err := h.storage.Stat(ctx, storagekeys.Video(*asset.Filename))
	if err == nil {
		return asset, info, http.StatusOK
	}
	if clientGone(err) {
		return nil, storage.FileInfo{}, statusClientClosed
	}
	if !errors.Is(err, fs.ErrNotExist) {
		h.log.Warn("playback stream: recheck file failed", "error", err, "id", id)
		return nil, storage.FileInfo{}, http.StatusServiceUnavailable
	}
	// A cached attached verdict can outlive a mount change. Confirm identity
	// before letting even the current absent artifact discard its ready row.
	if err := h.verifyStorage(ctx); !storage.CanRead(err) {
		if clientGone(err) {
			return nil, storage.FileInfo{}, statusClientClosed
		}
		return nil, storage.FileInfo{}, http.StatusServiceUnavailable
	}
	if err := h.repo.DeleteVideoPlaybackAsset(ctx, id); err != nil {
		h.log.Warn("demote stale playback asset failed", "video_id", id, "error", err)
	}
	return nil, storage.FileInfo{}, http.StatusNotFound
}

func (h *StreamHandler) streamPart(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid video id", http.StatusBadRequest)
		return
	}
	partIndex, err := strconv.ParseInt(chi.URLParam(r, "part"), 10, 32)
	if err != nil {
		http.Error(w, "invalid part index", http.StatusBadRequest)
		return
	}

	relPath, name, status, ok := h.resolveStreamablePart(r.Context(), id, int32(partIndex), "part")
	if !ok {
		http.Error(w, http.StatusText(status), status)
		return
	}
	if r.Method == http.MethodGet && isPlaybackSessionStart(r) {
		h.maybeKickBuild(id)
	}
	h.serveStorageFile(w, r, id, relPath, name)
}

// maybeKickBuild lets later views retry failures or evictions; the builder owns
// concurrent deduplication and the build outlives this HTTP request.
func (h *StreamHandler) maybeKickBuild(videoID int64) {
	if h.builder == nil || h.storageWriteUnavailable() != nil {
		return
	}
	if err := h.builder.StartBuild(context.Background(), videoID); err != nil {
		h.log.Warn("playback build admission failed", "video_id", videoID, "error", err)
	}
}

// serveStorageFile supports ranges and preserves the caller's Content-Disposition.
func (h *StreamHandler) serveStorageFile(w http.ResponseWriter, r *http.Request, videoID int64, relPath, name string) {
	if !h.requireReadableStorage(w) {
		return
	}
	info, err := h.storage.Stat(r.Context(), relPath)
	if err != nil {
		h.failStorageRead(w, r, videoID, relPath, "stat", err)
		return
	}
	h.serveStorageFileInfo(w, r, videoID, relPath, name, info)
}

// serveStorageFileInfo reuses a previous Stat result to avoid a second S3 HeadObject.
func (h *StreamHandler) serveStorageFileInfo(w http.ResponseWriter, r *http.Request, videoID int64, relPath, name string, info storage.FileInfo) {
	if !h.requireReadableStorage(w) {
		return
	}
	f, err := h.storage.Open(r.Context(), relPath)
	if err != nil {
		h.failStorageRead(w, r, videoID, relPath, "open", err)
		return
	}
	defer f.Close()

	// Serve audio-only .m4a as audio/mp4; the caller's stored Content-Type wins.
	if w.Header().Get("Content-Type") == "" {
		if contentType := contentTypeForRecordingFile(name); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
	}
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, name, info.ModTime, f)
}

// failStorageRead answers 404 for a definitive not-found and asks the missing
// marker to tombstone the recording; any other error is an outage and answers
// 503 so the player retries.
func (h *StreamHandler) failStorageRead(w http.ResponseWriter, r *http.Request, videoID int64, relPath, op string, err error) {
	if errors.Is(err, fs.ErrNotExist) {
		// An absent file on unattached storage says nothing about the recording:
		// answer as an outage and never ask for a tombstone.
		if err := h.storageUnavailable(); err != nil {
			h.log.Warn("video file missing while storage is not attached", "video_id", videoID, "path", relPath, "error", err)
			http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
			return
		}
		h.log.Warn("video file missing", "video_id", videoID, "path", relPath)
		if err := h.markMissing(r.Context(), videoID); err != nil {
			h.log.Warn("missing-media check failed", "video_id", videoID, "error", err)
			http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "video file missing", http.StatusNotFound)
		return
	}
	h.log.Error(op+" video file failed", "error", err, "video_id", videoID, "path", relPath)
	http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
}

// markMissing completes reconciliation before a 404, allowing an immediate
// refetch. Both owners and waiters honor request cancellation and a short
// deadline. No detached work survives the request, and overload returns 503.
func (h *StreamHandler) markMissing(parent context.Context, videoID int64) error {
	if h.missing == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, markMissingTimeout)
	defer cancel()
	h.missingMu.Lock()
	if h.missingChecks == nil {
		h.missingChecks = make(map[int64]*missingCheck)
	}
	for id, check := range h.missingChecks {
		if !check.checkedAt.IsZero() && time.Since(check.checkedAt) >= markMissingCooldown {
			delete(h.missingChecks, id)
		}
	}
	if check := h.missingChecks[videoID]; check != nil {
		h.missingMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-check.done:
			return check.err
		}
	}
	if h.activeMissingChecks >= maxActiveMissingChecks {
		h.missingMu.Unlock()
		return errors.New("missing-media checks busy")
	}
	if len(h.missingChecks) >= maxMissingChecks {
		var oldestID int64
		var oldest time.Time
		for id, check := range h.missingChecks {
			if !check.checkedAt.IsZero() && (oldest.IsZero() || check.checkedAt.Before(oldest)) {
				oldestID, oldest = id, check.checkedAt
			}
		}
		delete(h.missingChecks, oldestID)
	}
	check := &missingCheck{done: make(chan struct{})}
	h.missingChecks[videoID] = check
	h.activeMissingChecks++
	h.missingMu.Unlock()
	_, err := h.missing.MarkMissing(ctx, videoID)
	if err == nil {
		err = ctx.Err()
	}
	h.missingMu.Lock()
	check.err = err
	check.checkedAt = time.Now()
	h.activeMissingChecks--
	// Failures are retryable immediately; caching a timeout would make the next
	// HEAD return 404 without ever having completed reconciliation.
	if err != nil {
		delete(h.missingChecks, videoID)
	}
	close(check.done)
	h.missingMu.Unlock()
	return err
}

// isPlaybackSessionStart reports whether the request opens playback. Browsers
// use a plain GET or Range: bytes=0-; nonzero ranges are seeks or continuations.
func isPlaybackSessionStart(r *http.Request) bool {
	rng := r.Header.Get("Range")
	return rng == "" || strings.HasPrefix(rng, "bytes=0-")
}

func contentTypeForRecordingFile(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".mp4"):
		return "video/mp4"
	case strings.HasSuffix(lower, ".m4a"):
		return "audio/mp4"
	default:
		return ""
	}
}

// streamSignedPart validates the expiring signature before looking up media
// so invalid URLs reveal nothing about video or part existence.
func (h *StreamHandler) streamSignedPart(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid video id", http.StatusBadRequest)
		return
	}
	partIndex, err := strconv.ParseInt(chi.URLParam(r, "part"), 10, 32)
	if err != nil {
		http.Error(w, "invalid part index", http.StatusBadRequest)
		return
	}
	q := r.URL.Query()
	if h.verifier == nil ||
		h.verifier.Verify(id, int32(partIndex), q.Get(videodownload.ParamExpires), q.Get(videodownload.ParamSignature)) != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	relPath, name, status, ok := h.resolveStreamablePart(r.Context(), id, int32(partIndex), "signed part")
	if !ok {
		http.Error(w, http.StatusText(status), status)
		return
	}
	// Stored filenames contain only ASCII timestamps, Twitch logins and UUIDs,
	// so %q produces a valid RFC 6266 quoted-string. Non-ASCII filenames would
	// require a percent-encoded filename*=UTF-8'' parameter.
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	h.serveStorageFile(w, r, id, relPath, name)
}

func (h *StreamHandler) resolveStreamablePart(ctx context.Context, id int64, partIndex int32, logPrefix string) (relPath, name string, status int, ok bool) {
	video, err := h.repo.GetVideo(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return "", "", http.StatusNotFound, false
		}
		if clientGone(err) {
			return "", "", statusClientClosed, false
		}
		h.log.Error(logPrefix+": get video failed", "error", err, "id", id)
		return "", "", http.StatusInternalServerError, false
	}
	if video.Status != repository.VideoStatusDone {
		return "", "", http.StatusNotFound, false
	}
	if video.DeletedAt != nil {
		return "", "", http.StatusGone, false
	}

	parts, err := h.repo.ListVideoParts(ctx, id)
	if err != nil {
		if clientGone(err) {
			return "", "", statusClientClosed, false
		}
		h.log.Error(logPrefix+": list parts failed", "error", err, "id", id)
		return "", "", http.StatusInternalServerError, false
	}
	relPath, name, ok = partPath(video, parts, partIndex)
	if !ok {
		return "", "", http.StatusNotFound, false
	}
	return relPath, name, http.StatusOK, true
}

// partPath resolves only stored part references; filenames must never be guessed.
func partPath(_ *repository.Video, parts []repository.VideoPart, index int32) (relPath, name string, ok bool) {
	for _, p := range parts {
		if p.PartIndex == index {
			return storagekeys.Video(p.Filename), p.Filename, true
		}
	}
	return "", "", false
}

func (h *StreamHandler) serveThumbnail(w http.ResponseWriter, r *http.Request) {
	// chi's "/*" wildcard gives us everything after /thumbnails/
	path := chi.URLParam(r, "*")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	// Reject escapes from the thumbnails prefix even if the storage root permits them.
	path = strings.TrimLeft(path, "/")
	if strings.Contains(path, "..") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	relPath := "thumbnails/" + path
	if !h.requireReadableStorage(w) {
		return
	}

	f, err := h.storage.Open(ctx, relPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	info, err := h.storage.Stat(ctx, relPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	// The job UUID separates thumbnail names across recordings.
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	http.ServeContent(w, r, path, info.ModTime, f)
}

func (h *StreamHandler) requireReadableStorage(w http.ResponseWriter) bool {
	if err := h.storageUnavailable(); err != nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// storageUnavailable is the gate verdict for reads: read-only and full storage serve.
func (h *StreamHandler) storageUnavailable() error {
	err := h.storageWriteUnavailable()
	if storage.CanRead(err) {
		return nil
	}
	return err
}

func (h *StreamHandler) storageWriteUnavailable() error {
	if h.storage == nil {
		return nil
	}
	return h.storage.Ready()
}

func (h *StreamHandler) verifyStorage(ctx context.Context) error {
	if h.storage == nil {
		return nil
	}
	return h.storage.Verify(ctx)
}
