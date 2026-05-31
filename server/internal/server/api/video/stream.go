package video

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/videodownload"
	"github.com/go-chi/chi/v5"
)

// StreamHandler wires the Chi video/thumbnail streaming routes. These
// are NOT tRPC procedures — they need HTTP semantics (range requests,
// content-type negotiation, file streaming) that JSON RPC can't
// express.
type StreamHandler struct {
	repo     repository.Repository
	storage  storage.Storage
	verifier *videodownload.Verifier
	log      *slog.Logger
}

// NewStreamHandler creates a video streaming handler. verifier authorizes the
// signed per-part download route (SetupSignedRoutes); it may be nil, in which
// case that route rejects everything.
func NewStreamHandler(repo repository.Repository, store storage.Storage, verifier *videodownload.Verifier, log *slog.Logger) *StreamHandler {
	return &StreamHandler{
		repo:     repo,
		storage:  store,
		verifier: verifier,
		log:      log.With("domain", "video-stream"),
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
		r.Get("/thumbnails/*", h.serveThumbnail)
	})
}

// SetupSignedRoutes registers the signed per-part download route. Unlike
// SetupRoutes it is deliberately NOT behind the session middleware: a recording
// webhook consumer has no cookie, so the URL's HMAC signature and expiry are the
// authorization (verified in streamSignedPart). Register it on a router that is
// NOT wrapped in the auth middleware.
func (h *StreamHandler) SetupSignedRoutes(r chi.Router) {
	r.Get("/videos/{id}/parts/{part}/download", h.streamSignedPart)
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

	// Open through the storage layer so S3 Just Works alongside local.
	relPath, err := h.videoStreamPath(ctx, video)
	if err != nil {
		h.log.Error("resolve video path failed", "error", err, "id", id)
		http.Error(w, "video file unavailable", http.StatusNotFound)
		return
	}
	h.serveStorageFile(w, r, relPath, video.Filename+".mp4")
}

// serveStorageFile streams a storage-relative file with Range support. name is
// passed to http.ServeContent for content-type sniffing and is the suggested
// download filename. Any Content-Disposition the caller set on w is preserved.
func (h *StreamHandler) serveStorageFile(w http.ResponseWriter, r *http.Request, relPath, name string) {
	ctx := r.Context()
	f, err := h.storage.Open(ctx, relPath)
	if err != nil {
		h.log.Error("open video file failed", "error", err, "path", relPath)
		http.Error(w, "video file unavailable", http.StatusNotFound)
		return
	}
	defer f.Close()

	info, err := h.storage.Stat(ctx, relPath)
	if err != nil {
		h.log.Error("stat video file failed", "error", err, "path", relPath)
		http.Error(w, "video file unavailable", http.StatusNotFound)
		return
	}

	// http.ServeContent needs modtime for ETag/If-Modified-Since and the
	// display name for range/content handling. Set the known recording media
	// types ourselves so an audio-only .m4a part is served as audio/mp4 instead
	// of inheriting the browser-stream endpoint's video/mp4 default.
	if contentType := contentTypeForRecordingFile(name); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, name, info.ModTime, f)
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

	ctx := r.Context()
	video, err := h.repo.GetVideo(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.log.Error("signed part: get video failed", "error", err, "id", id)
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

	parts, err := h.repo.ListVideoParts(ctx, id)
	if err != nil {
		h.log.Error("signed part: list parts failed", "error", err, "id", id)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	relPath, name, ok := partPath(video, parts, int32(partIndex))
	if !ok {
		http.NotFound(w, r)
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
	h.serveStorageFile(w, r, relPath, name)
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
// Multi-part recordings serve only part 01: the watch flow is a
// single URL, and server-side concat across codec/container
// boundaries (the reason for the split) would corrupt output. The
// dashboard needs to sequence VideoResponse.Parts client-side; that
// work hasn't landed. Logged as a warning so any real occurrence is
// visible — variant drops are empirically rare.
//
// TODO(multi-part playback): the in-app player still plays only part
// 01 of a multi-part recording. The recording-webhook path already
// solved per-part addressing — streamSignedPart serves any single
// part by index (see SetupSignedRoutes + internal/videodownload) — so
// the remaining work is purely client-side: have the dashboard fetch
// VideoResponse.Parts and play them in sequence (or via a playlist),
// reusing /videos/{id}/parts/{index}/download for the bytes. Until
// then this endpoint stays single-file and logs the warning below.
// The MaxPartBytes/MaxPartSeconds split feature makes this more likely
// to be hit, so it should land alongside wider use of part splitting.
//
// Fallback to videos.filename + ".mp4" covers historical rows that
// predate the video_parts schema.
func (h *StreamHandler) videoStreamPath(ctx context.Context, v *repository.Video) (string, error) {
	parts, err := h.repo.ListVideoParts(ctx, v.ID)
	if err != nil {
		return "", err
	}
	if len(parts) == 0 {
		return storagekeys.Video(v.Filename + ".mp4"), nil
	}
	if len(parts) > 1 {
		h.log.Warn("multi-part recording streamed via single-file endpoint; only part 01 will play",
			"video_id", v.ID,
			"part_count", len(parts),
			"served_part", parts[0].Filename)
	}
	return storagekeys.Video(parts[0].Filename), nil
}
