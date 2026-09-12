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

	"github.com/befabri/replayvod/server/internal/recordinglock"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/videodownload"
	"github.com/befabri/replayvod/server/internal/waveform"
	"github.com/go-chi/chi/v5"
)

// statusClientClosed mirrors nginx's non-standard 499 "client closed request".
// Go's net/http has no constant for it. A browser playing a multi-part recording
// fires many overlapping Range GETs and aborts the in-flight ones on every seek
// or part switch; if such an abort lands while a request is still in its
// metadata phase (the GetVideo/ListVideoParts lookups before any bytes flow),
// the repo call returns context.Canceled. That's the client's doing, not a
// server fault, so we reply 499 — the body never reaches the gone client — and
// skip the ERROR log instead of emitting a misleading, alert-tripping 500.
const statusClientClosed = 499

func clientGone(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// PlaybackBuilder kicks a background build of a recording's single-file
// playback artifact. The streaming path calls it lazily (the first time a part
// is actually watched) so only viewed recordings cost a concat; it is the
// playbackcache service in production and may be nil (builds simply don't fire).
type PlaybackBuilder interface {
	StartBuild(ctx context.Context, videoID int64)
}

// MissingMarker tombstones a recording whose media is gone from storage; the
// streaming path calls it after a definitive not-found. It is the storagescan
// service in production and may be nil, in which case a missing file only
// answers 404.
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
	storage  storage.Storage
	verifier *videodownload.Verifier
	builder  PlaybackBuilder
	missing  MissingMarker
	gate     StorageGate
	log      *slog.Logger
	// missingMu protects both active checks and the bounded success cache.
	missingMu           sync.Mutex
	missingChecks       map[int64]*missingCheck
	activeMissingChecks int
	// warnedMultipart holds video IDs already logged about the multi-part
	// /stream single-file fallback, so the warning fires once per video rather
	// than on every request (a player issues a HEAD probe + many range GETs).
	warnedMultipart sync.Map
	// kickedBuild holds video IDs whose lazy playback-artifact build this process
	// has already kicked, so a view (HEAD probe + many range GETs across parts)
	// triggers StartBuild once rather than on every request. StartBuild is itself
	// idempotent; this just avoids the goroutine churn.
	kickedBuild sync.Map
	// waveformFlights deduplicates concurrent rebuilds of the same missing or
	// stale waveform artifact. Durable caching lives in object storage; this map
	// only holds active work and deletes each entry when that work completes.
	waveformFlights   *waveformFlights
	waveformGenerator waveform.Generator
	recordingLocks    *recordinglock.Locks
}

type StreamHandlerOption func(*StreamHandler)

func NewStreamHandler(repo repository.Repository, store storage.Storage, verifier *videodownload.Verifier, log *slog.Logger, opts ...StreamHandlerOption) *StreamHandler {
	h := &StreamHandler{
		repo:              repo,
		storage:           store,
		verifier:          verifier,
		log:               log.With("domain", "video-stream"),
		waveformFlights:   newWaveformFlights(),
		waveformGenerator: waveform.FFmpegGenerator{},
		recordingLocks:    &recordinglock.Locks{},
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

func WithStorageGate(g StorageGate) StreamHandlerOption {
	return func(h *StreamHandler) { h.gate = g }
}

func WithWaveformGenerator(g WaveformGenerator) StreamHandlerOption {
	return func(h *StreamHandler) {
		if g != nil {
			h.waveformGenerator = g
		}
	}
}

// WithRecordingLocks shares final artifact publication with recording deletion.
func WithRecordingLocks(locks *recordinglock.Locks) StreamHandlerOption {
	return func(h *StreamHandler) {
		if locks != nil {
			h.recordingLocks = locks
		}
	}
}

// SetupRoutes registers /videos/{id}/stream and /thumbnails/{path} on
// the given Chi router. Both require an authenticated session — a
// viewer at minimum — so we apply the auth middleware at the group
// level.
//
// authMiddleware is the session middleware; passed in rather than
// constructed here so the same instance is shared with the tRPC path.
func (h *StreamHandler) SetupRoutes(r chi.Router, authMiddleware func(http.Handler) http.Handler) {
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware)
		r.Get("/videos/{id}/stream", h.streamVideo)
		r.Head("/videos/{id}/stream", h.streamVideo)
		r.Get("/videos/{id}/playback/stream", h.streamPlayback)
		r.Head("/videos/{id}/playback/stream", h.streamPlayback)
		r.Get("/videos/{id}/parts/{part}/stream", h.streamPart)
		r.Head("/videos/{id}/parts/{part}/stream", h.streamPart)
		r.Get("/videos/{id}/waveform", h.streamAudioWaveform)
		r.Get("/thumbnails/*", h.serveThumbnail)
	})
}

// SetupSignedRoutes registers the signed per-part download route. Unlike
// SetupRoutes it is deliberately NOT behind the session middleware: a recording
// webhook consumer has no cookie, so the URL's HMAC signature and expiry are the
// authorization (verified in streamSignedPart). Register it on a router that is
// NOT wrapped in the auth middleware.
//
// HEAD is registered alongside GET because an unattended consumer routinely
// probes before it fetches: to size the transfer, read the filename out of
// Content-Disposition, or confirm the link has not expired before committing to
// a multi-gigabyte body. Both methods run the same signature verification and
// part resolution, so HEAD never reveals a part GET would refuse — it answers
// with the same status and headers and no body.
func (h *StreamHandler) SetupSignedRoutes(r chi.Router) {
	r.Get("/videos/{id}/parts/{part}/download", h.streamSignedPart)
	r.Head("/videos/{id}/parts/{part}/download", h.streamSignedPart)
}

// streamVideo serves a downloaded MP4 with HTTP Range support so the browser
// can seek. http.ServeContent does all the heavy lifting (206 Partial
// Content, Content-Range headers, conditional requests).
func (h *StreamHandler) streamVideo(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
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
		h.log.Error("get video failed", "error", err, "id", id)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Only DONE videos are streamable. PENDING/RUNNING don't have a file
	// yet; FAILED never will.
	if video.Status != repository.VideoStatusDone {
		http.Error(w, "video not available", http.StatusNotFound)
		return
	}
	if video.DeletedAt != nil {
		http.Error(w, "video deleted", http.StatusGone)
		return
	}

	parts, err := h.repo.ListVideoParts(ctx, video.ID)
	if err != nil {
		if clientGone(err) {
			http.Error(w, "client closed request", statusClientClosed)
			return
		}
		h.log.Error("list video parts failed", "error", err, "id", id)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Open through the storage layer so S3 Just Works alongside local.
	relPath, name := h.videoStreamPath(video, parts)
	if len(parts) > 1 {
		// Legacy single-URL fallback: /stream plays only part 01. The dashboard
		// uses /playback/stream or /parts/{n}/stream instead; this path exists
		// for external clients / old bookmarks. Warn once per video — a player
		// fires a HEAD probe plus many range GETs, so per-request logging would
		// flood the logs for a single view.
		if _, warned := h.warnedMultipart.LoadOrStore(video.ID, struct{}{}); !warned {
			h.log.Warn("multi-part recording streamed via single-file fallback; only part 01 plays on /stream (use /playback/stream or /parts/{n}/stream)",
				"video_id", video.ID,
				"part_count", len(parts),
				"served_part", name)
		}
	}
	h.serveStorageFile(w, r, video.ID, relPath, name)
}

// streamPlayback serves only a finished playback artifact. It never builds or
// concatenates on request; clients fall back to /parts/{n}/stream until
// video.getById reports a ready artifact.
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
	// Confirm the file is present before doing anything else. If a ready row
	// outlived its file (e.g. a retention pass that purged the object but whose
	// row delete then failed), drop the row so video.getById stops advertising
	// the artifact and the watch page falls back to /parts; the reconciler
	// rebuilds it. Only act on a definitive not-found.
	info, statErr := h.storage.Stat(ctx, relPath)
	switch {
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
		// Transient stat error: let serveStorageFile re-stat and surface/log it.
		h.serveStorageFile(w, r, id, relPath, *asset.Filename)
		return
	}
	if r.Method == http.MethodGet && isPlaybackSessionStart(r) {
		// LRU bump. A playing client issues a HEAD probe plus many Range GETs per
		// view/seek; touching on each would be a write storm (and on SQLite would
		// serialize against live recording writes). Touch only at the start of a
		// playback session.
		if err := h.repo.TouchVideoPlaybackAsset(ctx, id); err != nil {
			h.log.Warn("touch playback asset failed", "video_id", id, "error", err)
		}
	}
	// Serve the recorded Content-Type rather than re-deriving it from the
	// filename suffix, so the stored mime_type is authoritative. Reuse the
	// FileInfo from the stale-row check so we don't Stat the object twice.
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
	unlock, err := h.recordingLocks.Lock(ctx, id)
	if err != nil {
		return nil, storage.FileInfo{}, statusClientClosed
	}
	defer unlock()
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

// streamPart serves one recording part through the authenticated dashboard
// session path. The watch player uses this route to sequence multi-part
// recordings client-side without relying on signed download URLs or forcing a
// Content-Disposition attachment.
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
	// Someone is actually watching this recording: lazily kick the single-file
	// playback artifact build (once per video for this process). The builder
	// no-ops for single-part recordings, already-built ones, and oversized ones;
	// the player keeps sequencing parts meanwhile and upgrades to the artifact
	// once it's ready. Only the authenticated dashboard path triggers builds —
	// not the signed per-part download route consumers use.
	h.maybeKickBuild(id)
	h.serveStorageFile(w, r, id, relPath, name)
}

// maybeKickBuild starts the lazy playback-artifact build for videoID at most
// once per process. The detached context is deliberate: the build outlives this
// HTTP request (whose context cancels the instant the range read finishes).
func (h *StreamHandler) maybeKickBuild(videoID int64) {
	if h.builder == nil || h.storageWriteUnavailable() != nil {
		return
	}
	if _, kicked := h.kickedBuild.LoadOrStore(videoID, struct{}{}); kicked {
		return
	}
	h.builder.StartBuild(context.Background(), videoID)
}

// serveStorageFile streams a storage-relative file with Range support. name is
// passed to http.ServeContent for content-type sniffing and is the suggested
// download filename. Any Content-Disposition the caller set on w is preserved.
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

// serveStorageFileInfo is serveStorageFile for a caller that has already
// Stat'd relPath, so it doesn't repeat the Stat — on S3 that's one fewer
// HeadObject per request (streamPlayback already Stats for its stale-row check).
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

	// http.ServeContent needs modtime for ETag/If-Modified-Since and the
	// display name for range/content handling. Set the known recording media
	// types ourselves so an audio-only .m4a part is served as audio/mp4 instead
	// of inheriting the browser-stream endpoint's video/mp4 default. A caller
	// that already set Content-Type (e.g. streamPlayback's stored mime) wins.
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

// isPlaybackSessionStart reports whether r is the opening request of a playback
// session rather than a seek or a continuation chunk. An HTML5 <video> element
// opens with either a plain GET or `Range: bytes=0-` (browsers commonly probe
// range support up front), while seeks and continuation reads request a
// non-zero start. Bumping the LRU clock only here captures a real "watched now"
// signal without a write per range chunk — and, crucially, without freezing the
// clock for the common case where the browser always sends a Range.
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

// streamSignedPart serves one recorded part's bytes for a signed, expiring,
// unauthenticated URL (videodownload mints these for recording-webhook
// consumers). The query-string HMAC signature and expiry are the authorization
// — there is no session — so a bad or expired signature is a flat 403 that
// reveals nothing about whether the video or the part exists.
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
	// Force a download (not inline render) for the unattended consumer. name is
	// always ASCII: partPath returns either buildFilename's output
	// (<UTC-timestamp>-<lowercase-login>-<hex-jobID>-partNN.<ext>) or the legacy
	// videos.filename + ".mp4", and a Twitch login is [a-z0-9_]. So fmt %q is a
	// valid RFC 6266 quoted-string here (it escapes any " or \, and nothing in
	// that charset needs more). If a filename ever carries non-ASCII, %q emits
	// \uXXXX escapes a client can't decode — that case needs a
	// filename*=UTF-8'' percent-encoded form alongside the plain filename=.
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

// partPath resolves the storage-relative path and suggested filename for one
// part index. A recording with no part rows is a historical single-file row:
// only index 0 is valid and maps to the legacy videos.filename + ".mp4".
func partPath(v *repository.Video, parts []repository.VideoPart, index int32) (relPath, name string, ok bool) {
	if len(parts) == 0 {
		if index == 0 {
			return storagekeys.Video(v.Filename + ".mp4"), v.Filename + ".mp4", true
		}
		return "", "", false
	}
	for _, p := range parts {
		if p.PartIndex == index {
			return storagekeys.Video(p.Filename), p.Filename, true
		}
	}
	return "", "", false
}

// serveThumbnail streams the thumbnail JPEG directly from the thumbnails
// subtree. We strip the /thumbnails/ prefix so the URL path maps directly to
// a storage-relative path.
//
// Unlike videos, thumbnails don't need range support — they're small enough
// to serve whole. io.Copy is fine here.
func (h *StreamHandler) serveThumbnail(w http.ResponseWriter, r *http.Request) {
	// chi's "/*" wildcard gives us everything after /thumbnails/
	path := chi.URLParam(r, "*")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	// Prevent escaping the thumbnails subdir — strip any leading slashes and
	// reject path traversal attempts. The storage layer does its own check
	// too (defense in depth).
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
	// Thumbnails are content-addressable (filename includes the job UUID),
	// so once generated they never change. A long immutable cache is safe.
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	http.ServeContent(w, r, path, info.ModTime, f)
}

// videoStreamPath reads from video_parts.filename — the
// authoritative source of the on-disk path.
//
// Multi-part recordings fall back to part 01 on /stream. The dashboard uses
// either a ready playback artifact or the authenticated part route instead, so
// /stream stays a cheap legacy endpoint and never performs request-time concat.
//
// Fallback to videos.filename + ".mp4" covers historical rows that
// predate the video_parts schema.
func (h *StreamHandler) videoStreamPath(v *repository.Video, parts []repository.VideoPart) (relPath, name string) {
	if len(parts) == 0 {
		name := v.Filename + ".mp4"
		return storagekeys.Video(name), name
	}
	return storagekeys.Video(parts[0].Filename), parts[0].Filename
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
	if h.gate == nil {
		return nil
	}
	return h.gate.Ready()
}

func (h *StreamHandler) verifyStorage(ctx context.Context) error {
	if h.gate == nil {
		return nil
	}
	return h.gate.Verify(ctx)
}
