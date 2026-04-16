package video

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/go-chi/chi/v5"
)

// StreamHandler wires the Chi video/thumbnail streaming routes. These
// are NOT tRPC procedures — they need HTTP semantics (range requests,
// content-type negotiation, file streaming) that JSON RPC can't
// express.
type StreamHandler struct {
	repo    repository.Repository
	storage storage.Storage
	log     *slog.Logger
}

// NewStreamHandler creates a video streaming handler.
func NewStreamHandler(repo repository.Repository, store storage.Storage, log *slog.Logger) *StreamHandler {
	return &StreamHandler{
		repo:    repo,
		storage: store,
		log:     log.With("domain", "video-stream"),
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
	// display name for Content-Type sniffing via extension.
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, video.Filename+".mp4", info.ModTime, f)
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

// videoStreamPath returns the storage-relative path of the video to
// stream. Phase 6f stores one video_parts row per part with the
// authoritative `<base>-partNN.mp4` filename — read it directly
// rather than reconstructing the path from videos.filename + ".mp4".
//
// **Multi-part recordings serve only part 01.** The watch flow is
// a single /videos/{id}/stream URL; the browser <video> element
// can't iterate parts on its own, and server-side concat across
// codec/container boundaries (the spec's reason for the part split
// in the first place) would either fail or produce a broken file.
//
// The intended UX path: dashboard reads VideoResponse.Parts from
// video.getById (Phase 6f.3) and either plays them sequentially via
// the `ended` event or shows a part picker. That UI work isn't part
// of 6f's server scope. Until it lands, a recording that crossed
// a variant drop will play back truncated at the first boundary.
//
// Variant drops are empirically rare — most VODs have one part —
// so this gap affects approximately zero recordings in practice.
// We log a warning when serving a multi-part video so the operator
// knows when (if ever) a real recording trips it.
//
// Pre-6f historical videos: their video_parts rows carry filenames
// without the -partNN suffix (they predate the suffix convention).
// The video_parts row is still authoritative — we read its Filename
// verbatim and prepend "videos/". Old recordings keep streaming;
// new ones use the suffixed path.
//
// Fallback: if no video_parts row exists at all (which shouldn't
// happen for any DONE video — even pre-6f recordings created a
// part row), reconstruct the legacy path from videos.filename so we
// don't 404 on a recording that's otherwise readable.
func (h *StreamHandler) videoStreamPath(ctx context.Context, v *repository.Video) (string, error) {
	parts, err := h.repo.ListVideoParts(ctx, v.ID)
	if err != nil {
		return "", err
	}
	if len(parts) == 0 {
		return "videos/" + v.Filename + ".mp4", nil
	}
	if len(parts) > 1 {
		h.log.Warn("multi-part recording streamed via single-file endpoint; only part 01 will play",
			"video_id", v.ID,
			"part_count", len(parts),
			"served_part", parts[0].Filename)
	}
	return "videos/" + parts[0].Filename, nil
}
