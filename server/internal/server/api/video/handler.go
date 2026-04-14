package video

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/trpcgo"
)

// Handler is the tRPC adapter for the video domain.
type Handler struct {
	video    *Service
	download *DownloadService
	storage  storage.Storage
	log      *slog.Logger
}

// NewHandler wires a handler around the two video domain services.
// storage is used by the Snapshots endpoint to probe for the
// hover-preview images saved during recording.
func NewHandler(video *Service, download *DownloadService, store storage.Storage, log *slog.Logger) *Handler {
	return &Handler{
		video:    video,
		download: download,
		storage:  store,
		log:      log.With("domain", "video-api"),
	}
}

// VideoResponse is the wire shape for a video record. broadcaster_*
// and profile_image_url come from a JOIN-equivalent channel lookup
// that the service layer does in bulk once per response — the frontend
// renders the video card's avatar + channel link without a per-row
// channel.getById, which would trip trpcgo's batching ceiling on a
// full grid.
type VideoResponse struct {
	ID              int64      `json:"id"`
	JobID           string     `json:"job_id"`
	Filename        string     `json:"filename"`
	DisplayName     string     `json:"display_name"`
	// Title is the stream title at download-start time. Empty when
	// Twitch didn't surface a title (manual trigger on an offline
	// channel); the UI falls back to display_name in that case.
	Title           string     `json:"title"`
	Status          string     `json:"status"`
	// CompletionKind distinguishes clean-end from partial/cancelled
	// recordings. See repository.CompletionKind* constants. The UI
	// renders a secondary badge (PARTIAL) for DONE+partial and
	// replaces the FAILED badge with CANCELLED when the operator
	// explicitly cancelled.
	CompletionKind   string     `json:"completion_kind"`
	Quality          string     `json:"quality"`
	BroadcasterID    string     `json:"broadcaster_id"`
	// BroadcasterLogin / BroadcasterName / ProfileImageURL come from
	// the channels mirror. When the broadcaster isn't locally synced
	// (rare but possible for historical videos) these are empty and
	// the frontend falls back to DisplayName + initials avatar.
	BroadcasterLogin string     `json:"broadcaster_login,omitempty"`
	BroadcasterName  string     `json:"broadcaster_name,omitempty"`
	ProfileImageURL  *string    `json:"profile_image_url,omitempty"`
	StreamID         *string    `json:"stream_id,omitempty"`
	ViewerCount      int64      `json:"viewer_count"`
	Language         string     `json:"language"`
	DurationSeconds  *float64   `json:"duration_seconds,omitempty"`
	SizeBytes        *int64     `json:"size_bytes,omitempty"`
	Thumbnail        *string    `json:"thumbnail,omitempty"`
	Error            *string    `json:"error,omitempty"`
	StartDownloadAt  time.Time  `json:"start_download_at"`
	DownloadedAt     *time.Time `json:"downloaded_at,omitempty"`
}

func toVideoResponse(v *repository.Video, ch *repository.Channel) VideoResponse {
	resp := VideoResponse{
		ID:              v.ID,
		JobID:           v.JobID,
		Filename:        v.Filename,
		DisplayName:     v.DisplayName,
		Title:           v.Title,
		Status:          v.Status,
		CompletionKind:  v.CompletionKind,
		Quality:         v.Quality,
		BroadcasterID:   v.BroadcasterID,
		StreamID:        v.StreamID,
		ViewerCount:     v.ViewerCount,
		Language:        v.Language,
		DurationSeconds: v.DurationSeconds,
		SizeBytes:       v.SizeBytes,
		Thumbnail:       v.Thumbnail,
		Error:           v.Error,
		StartDownloadAt: v.StartDownloadAt,
		DownloadedAt:    v.DownloadedAt,
	}
	if ch != nil {
		resp.BroadcasterLogin = ch.BroadcasterLogin
		resp.BroadcasterName = ch.BroadcasterName
		resp.ProfileImageURL = ch.ProfileImageURL
	}
	return resp
}

// toVideoResponses enriches every row with its broadcaster's display
// metadata via a single ListChannelsByIDs lookup. Replaces the previous
// per-row transform — the frontend's video card used to paper over the
// missing fields with a per-card channel.getById call, which on a
// 10-row grid took the batch over trpcgo's 10-procedure ceiling and
// 400'd the whole dashboard.
func (h *Handler) toVideoResponses(ctx context.Context, vs []repository.Video) []VideoResponse {
	channels := h.video.ChannelsByBroadcasterIDs(ctx, vs)
	out := make([]VideoResponse, len(vs))
	for i := range vs {
		out[i] = toVideoResponse(&vs[i], channels[vs[i].BroadcasterID])
	}
	return out
}

// ListInput carries the pagination + filter + sort dimensions for
// video.list. Sort/Order are whitelisted at the validator; the SQL
// defaults to start_download_at DESC when either is empty.
type ListInput struct {
	Limit  int    `json:"limit" validate:"min=0,max=200"`
	Offset int    `json:"offset" validate:"min=0"`
	Status string `json:"status,omitempty" validate:"omitempty,oneof=PENDING RUNNING DONE FAILED"`
	Sort   string `json:"sort,omitempty" validate:"omitempty,oneof=created_at duration size channel"`
	Order  string `json:"order,omitempty" validate:"omitempty,oneof=asc desc"`
}

func (h *Handler) List(ctx context.Context, input ListInput) ([]VideoResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	// Default to desc when the caller specified a sort column but no
	// direction. Without this the opts.SortKey() fallback would silently
	// revert to default ordering, which is a surprising outcome for a
	// client that thinks it asked for duration/size/channel sorting.
	order := input.Order
	if input.Sort != "" && order == "" {
		order = "desc"
	}
	vids, err := h.video.List(ctx, repository.ListVideosOpts{
		Status: input.Status,
		Sort:   input.Sort,
		Order:  order,
		Limit:  limit,
		Offset: input.Offset,
	})
	if err != nil {
		h.log.Error("list videos", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list videos")
	}
	return h.toVideoResponses(ctx, vids), nil
}

type GetByIDInput struct {
	ID int64 `json:"id" validate:"required"`
}

// TitleItem is the wire shape for one title in a video's history.
// ID is the deduplicated titles row; Name is the broadcast label.
// Listed in the order the titles were first linked to the video
// (opening title first, change events after).
type TitleItem struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type TitlesInput struct {
	VideoID int64 `json:"video_id" validate:"required"`
}

// VideoCategory is the wire shape for one category in a video's
// history. Parallels TitleItem — opening category first, mid-stream
// game switches after, distinct rows only.
type VideoCategory struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	BoxArtURL *string `json:"box_art_url,omitempty"`
}

type CategoriesInput struct {
	VideoID int64 `json:"video_id" validate:"required"`
}

// SnapshotsInput identifies a video whose live-recording snapshots
// should be listed.
type SnapshotsInput struct {
	VideoID int64 `json:"video_id" validate:"required"`
}

// Snapshots returns the ordered list of snapshot paths (relative to
// the storage root) captured during a recording. The frontend uses
// this to cycle through thumbnails on hover.
func (h *Handler) Snapshots(ctx context.Context, input SnapshotsInput) ([]string, error) {
	paths, err := h.video.ListSnapshots(ctx, h.storage, input.VideoID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, trpcgo.NewError(trpcgo.CodeNotFound, "video not found")
		}
		h.log.Error("list snapshots", "video_id", input.VideoID, "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list snapshots")
	}
	return paths, nil
}

// Titles returns every title recorded for a video, in link order.
// Empty result when title tracking is disabled or the recording is
// too short for any ticks to fire — the UI treats empty as "no
// history available; use display_name + videos.title."
func (h *Handler) Titles(ctx context.Context, input TitlesInput) ([]TitleItem, error) {
	rows, err := h.video.Titles(ctx, input.VideoID)
	if err != nil {
		h.log.Error("list titles for video", "video_id", input.VideoID, "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list titles")
	}
	out := make([]TitleItem, len(rows))
	for i, r := range rows {
		out[i] = TitleItem{ID: r.ID, Name: r.Name}
	}
	return out, nil
}

// Categories returns every category recorded for a video, in link
// order. Empty result for pre-category-tracking recordings or when
// the stream had no game set — the UI uses empty to hide the
// category history row / inline badges entirely.
func (h *Handler) Categories(ctx context.Context, input CategoriesInput) ([]VideoCategory, error) {
	rows, err := h.video.Categories(ctx, input.VideoID)
	if err != nil {
		h.log.Error("list categories for video", "video_id", input.VideoID, "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list categories")
	}
	out := make([]VideoCategory, len(rows))
	for i, r := range rows {
		out[i] = VideoCategory{
			ID:        r.ID,
			Name:      r.Name,
			BoxArtURL: r.BoxArtURL,
		}
	}
	return out, nil
}

func (h *Handler) GetByID(ctx context.Context, input GetByIDInput) (VideoResponse, error) {
	v, err := h.video.GetByID(ctx, input.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return VideoResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "video not found")
		}
		h.log.Error("get video", "error", err)
		return VideoResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to get video")
	}
	// Single-row enrichment through the same bulk helper so GetByID's
	// wire shape matches List's — the Watch page's VideoInfo no longer
	// needs a separate channel.getById to render the header.
	channels := h.video.ChannelsByBroadcasterIDs(ctx, []repository.Video{*v})
	return toVideoResponse(v, channels[v.BroadcasterID]), nil
}

type ByBroadcasterInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
	Limit         int    `json:"limit" validate:"min=0,max=200"`
	Offset        int    `json:"offset" validate:"min=0"`
}

func (h *Handler) ByBroadcaster(ctx context.Context, input ByBroadcasterInput) ([]VideoResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	vids, err := h.video.ListByBroadcaster(ctx, input.BroadcasterID, limit, input.Offset)
	if err != nil {
		h.log.Error("list videos by broadcaster", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list videos")
	}
	return h.toVideoResponses(ctx, vids), nil
}

type ByCategoryInput struct {
	CategoryID string `json:"category_id" validate:"required"`
	Limit      int    `json:"limit" validate:"min=0,max=200"`
	Offset     int    `json:"offset" validate:"min=0"`
}

func (h *Handler) ByCategory(ctx context.Context, input ByCategoryInput) ([]VideoResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	vids, err := h.video.ListByCategory(ctx, input.CategoryID, limit, input.Offset)
	if err != nil {
		h.log.Error("list videos by category", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list videos")
	}
	return h.toVideoResponses(ctx, vids), nil
}

type StatsBucket struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

type StatisticsResponse struct {
	Total         int64         `json:"total"`
	TotalSize     int64         `json:"total_size"`
	TotalDuration float64       `json:"total_duration_seconds"`
	ByStatus      []StatsBucket `json:"by_status"`
}

func (h *Handler) Statistics(ctx context.Context) (StatisticsResponse, error) {
	stats, err := h.video.Stats(ctx)
	if err != nil {
		h.log.Error("video statistics", "error", err)
		return StatisticsResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load statistics")
	}
	out := StatisticsResponse{
		Total:         stats.Totals.Total,
		TotalSize:     stats.Totals.TotalSize,
		TotalDuration: stats.Totals.TotalDuration,
		ByStatus:      make([]StatsBucket, len(stats.ByStatus)),
	}
	for i, b := range stats.ByStatus {
		out.ByStatus[i] = StatsBucket{Status: b.Status, Count: b.Count}
	}
	return out, nil
}

// TriggerDownloadInput starts a manual download for a live broadcaster.
// RecordingType + ForceH264 are accepted at the API boundary so the
// dashboard can send them; the native HLS downloader (Phase 4+) will
// consume them at Stage 3 variant selection. Until then they are
// recorded on the `videos` row via VideoInput but otherwise ignored.
type TriggerDownloadInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
	RecordingType string `json:"recording_type,omitempty" validate:"omitempty,oneof=video audio"`
	Quality       string `json:"quality,omitempty" validate:"omitempty,oneof=LOW MEDIUM HIGH"`
	ForceH264     bool   `json:"force_h264,omitempty"`
}

// TriggerDownloadResponse returns the job id so the UI can subscribe
// to progress.
type TriggerDownloadResponse struct {
	JobID   string `json:"job_id"`
	VideoID int64  `json:"video_id"`
}

func (h *Handler) TriggerDownload(ctx context.Context, input TriggerDownloadInput) (TriggerDownloadResponse, error) {
	user := middleware.GetUser(ctx)
	tokens := middleware.GetTokens(ctx)
	if user == nil || tokens == nil {
		return TriggerDownloadResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	result, err := h.download.Trigger(ctx, TriggerInput{
		BroadcasterID:   input.BroadcasterID,
		RecordingType:   input.RecordingType,
		Quality:         input.Quality,
		ForceH264:       input.ForceH264,
		UserID:          user.ID,
		UserAccessToken: tokens.AccessToken,
	})
	if err != nil {
		if errors.Is(err, ErrChannelNotSynced) {
			return TriggerDownloadResponse{}, trpcgo.NewError(trpcgo.CodeNotFound,
				"channel not synced — run channel.syncFromTwitch first")
		}
		h.log.Error("trigger download", "error", err, "broadcaster_id", input.BroadcasterID)
		return TriggerDownloadResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError,
			"failed to start download: "+err.Error())
	}
	return TriggerDownloadResponse{JobID: result.JobID, VideoID: result.VideoID}, nil
}

type CancelInput struct {
	JobID string `json:"job_id" validate:"required"`
}

type OK struct {
	OK bool `json:"ok"`
}

func (h *Handler) Cancel(ctx context.Context, input CancelInput) (OK, error) {
	h.download.Cancel(ctx, input.JobID)
	return OK{OK: true}, nil
}

type DownloadProgressInput struct {
	JobID string `json:"job_id" validate:"required"`
}

// ProgressEvent is the wire shape for a download progress update.
// Matches downloader.Progress but pinned to a JSON-stable schema.
//
// Cumulative semantics: each event fully replaces the previous,
// so subscribers that miss intermediate events (slow render, SSE
// reconnect) stay consistent once they receive the next one.
type ProgressEvent struct {
	JobID          string  `json:"job_id"`
	PartIndex      int     `json:"part_index"`
	Stage          string  `json:"stage"`
	BytesWritten   int64   `json:"bytes_written"`
	SegmentsDone   int64   `json:"segments_done"`
	SegmentsGaps   int64   `json:"segments_gaps"`
	SegmentsAdGaps int64   `json:"segments_ad_gaps"`
	SegmentsTotal  int64   `json:"segments_total"`
	Percent        float64 `json:"percent"`
	Speed          string  `json:"speed,omitempty"`
	ETA            string  `json:"eta,omitempty"`
	Quality        string  `json:"quality,omitempty"`
	Codec          string  `json:"codec,omitempty"`
	RecordingType  string  `json:"recording_type,omitempty"`
}

// DownloadProgress streams Progress events for a running download
// via SSE. Returns an empty closed channel if the job is not (or no
// longer) active — this collapses "already done" and "never existed"
// into a single tidy closed-stream path the client handles naturally.
//
// The trpcgo middleware chain runs before this handler, so an expired
// session 401s on subscribe rather than hanging an open SSE forever.
// The SSE transport handles client disconnects by cancelling ctx,
// which propagates through to us; the downloader publishes with a
// non-blocking select so a dropped subscriber never wedges its writes.
func (h *Handler) DownloadProgress(ctx context.Context, input DownloadProgressInput) (<-chan ProgressEvent, error) {
	src := h.download.Subscribe(input.JobID)
	if src == nil {
		ch := make(chan ProgressEvent)
		close(ch)
		return ch, nil
	}

	out := make(chan ProgressEvent, 16)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case p, ok := <-src:
				if !ok {
					return
				}
				select {
				case out <- ProgressEvent{
					JobID:          p.JobID,
					PartIndex:      p.PartIndex,
					Stage:          p.Stage,
					BytesWritten:   p.BytesWritten,
					SegmentsDone:   p.SegmentsDone,
					SegmentsGaps:   p.SegmentsGaps,
					SegmentsAdGaps: p.SegmentsAdGaps,
					SegmentsTotal:  p.SegmentsTotal,
					Percent:        p.Percent,
					Speed:          p.Speed,
					ETA:            p.ETA,
					Quality:        p.Quality,
					Codec:          p.Codec,
					RecordingType:  p.RecordingType,
				}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}
