package video

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/twitch"
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
	ID          int64  `json:"id"`
	JobID       string `json:"job_id"`
	Filename    string `json:"filename"`
	DisplayName string `json:"display_name"`
	// Title is the stream title at download-start time. Empty when
	// Twitch didn't surface a title (manual trigger on an offline
	// channel); the UI falls back to display_name in that case.
	Title  string `json:"title"`
	Status string `json:"status"`
	// CompletionKind distinguishes clean-end from partial/cancelled
	// recordings. See repository.CompletionKind* constants. The UI
	// renders a secondary badge (PARTIAL) for DONE+partial and
	// replaces the FAILED badge with CANCELLED when the operator
	// explicitly cancelled.
	CompletionKind string `json:"completion_kind"`
	// Truncated is true when the recording stopped before the
	// broadcast ended — operator cancel, mid-run failure, or a clean
	// finalize that never observed EXT-X-ENDLIST. Orthogonal to
	// CompletionKind. The dashboard's videos page uses this to
	// distinguish "we have the whole stream" from "we only have the
	// part of the broadcast we recorded for."
	Truncated bool `json:"truncated"`
	// Quality is the display label for the selected recorded rendition
	// when Stage 3 has picked one (e.g. 1080p60). Before that it falls
	// back to the requested quality enum (HIGH/MEDIUM/LOW).
	Quality       string   `json:"quality"`
	FPS           *float64 `json:"fps,omitempty"`
	BroadcasterID string   `json:"broadcaster_id"`
	// BroadcasterLogin / BroadcasterName / ProfileImageURL come from
	// the channels mirror. When the broadcaster isn't locally synced
	// (rare but possible for historical videos) these are empty and
	// the frontend falls back to DisplayName + initials avatar.
	BroadcasterLogin         string     `json:"broadcaster_login,omitempty"`
	BroadcasterName          string     `json:"broadcaster_name,omitempty"`
	ProfileImageURL          *string    `json:"profile_image_url,omitempty"`
	PrimaryCategoryID        *string    `json:"primary_category_id,omitempty"`
	PrimaryCategoryName      *string    `json:"primary_category_name,omitempty"`
	PrimaryCategoryBoxArtURL *string    `json:"primary_category_box_art_url,omitempty"`
	StreamID                 *string    `json:"stream_id,omitempty"`
	ViewerCount              int64      `json:"viewer_count"`
	Language                 string     `json:"language"`
	DurationSeconds          *float64   `json:"duration_seconds,omitempty"`
	SizeBytes                *int64     `json:"size_bytes,omitempty"`
	Thumbnail                *string    `json:"thumbnail,omitempty"`
	Error                    *string    `json:"error,omitempty"`
	StartDownloadAt          time.Time  `json:"start_download_at"`
	DownloadedAt             *time.Time `json:"downloaded_at,omitempty"`
	// Parts is populated only by GetByID — list endpoints skip it
	// to avoid N+1 queries on grid views.
	Parts []VideoPartResponse `json:"parts,omitempty"`
}

// VideoPartResponse mirrors repository.VideoPart with stable JSON tags.
type VideoPartResponse struct {
	ID              int64    `json:"id"`
	PartIndex       int32    `json:"part_index"`
	Filename        string   `json:"filename"`
	Quality         string   `json:"quality"`
	FPS             *float64 `json:"fps,omitempty"`
	Codec           string   `json:"codec"`
	SegmentFormat   string   `json:"segment_format"`
	DurationSeconds float64  `json:"duration_seconds"`
	SizeBytes       int64    `json:"size_bytes"`
	Thumbnail       *string  `json:"thumbnail,omitempty"`
	StartMediaSeq   int64    `json:"start_media_seq"`
	EndMediaSeq     *int64   `json:"end_media_seq,omitempty"`
}

func toVideoPartResponses(parts []repository.VideoPart) []VideoPartResponse {
	out := make([]VideoPartResponse, len(parts))
	for i, p := range parts {
		out[i] = VideoPartResponse{
			ID:              p.ID,
			PartIndex:       p.PartIndex,
			Filename:        p.Filename,
			Quality:         formatQualityLabel(p.Quality, p.FPS),
			FPS:             p.FPS,
			Codec:           p.Codec,
			SegmentFormat:   p.SegmentFormat,
			DurationSeconds: p.DurationSeconds,
			SizeBytes:       p.SizeBytes,
			Thumbnail:       p.Thumbnail,
			StartMediaSeq:   p.StartMediaSeq,
			EndMediaSeq:     p.EndMediaSeq,
		}
	}
	return out
}

func toVideoResponse(v *repository.Video, ch *repository.Channel, primaryCategory *repository.Category) VideoResponse {
	resp := VideoResponse{
		ID:              v.ID,
		JobID:           v.JobID,
		Filename:        v.Filename,
		DisplayName:     v.DisplayName,
		Title:           v.Title,
		Status:          v.Status,
		CompletionKind:  v.CompletionKind,
		Truncated:       v.Truncated,
		Quality:         formatVideoQualityLabel(v),
		FPS:             v.SelectedFPS,
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
	if primaryCategory != nil {
		resp.PrimaryCategoryID = &primaryCategory.ID
		resp.PrimaryCategoryName = &primaryCategory.Name
		resp.PrimaryCategoryBoxArtURL = primaryCategory.BoxArtURL
	}
	return resp
}

func formatVideoQualityLabel(v *repository.Video) string {
	if v == nil {
		return ""
	}
	if v.SelectedQuality != nil && *v.SelectedQuality != "" {
		return formatQualityLabel(*v.SelectedQuality, v.SelectedFPS)
	}
	return formatQualityLabel(v.Quality, nil)
}

func formatQualityLabel(quality string, fps *float64) string {
	if quality == "" {
		return ""
	}
	if quality == "audio_only" {
		return quality
	}
	if _, err := strconv.Atoi(quality); err != nil {
		return quality
	}
	label := quality + "p"
	if fps == nil || *fps <= 0 {
		return label
	}
	return label + strconv.Itoa(int(math.Round(*fps)))
}

// toVideoResponses enriches every row with its broadcaster's display
// metadata via a single ListChannelsByIDs lookup. Replaces the previous
// per-row transform — the frontend's video card used to paper over the
// missing fields with a per-card channel.getById call, which on a
// 10-row grid took the batch over trpcgo's 10-procedure ceiling and
// 400'd the whole dashboard.
func (h *Handler) toVideoResponses(ctx context.Context, vs []repository.Video) []VideoResponse {
	channels := h.video.ChannelsByBroadcasterIDs(ctx, vs)
	primaryCategories := h.video.PrimaryCategoriesByVideoIDs(ctx, vs)
	out := make([]VideoResponse, len(vs))
	for i := range vs {
		out[i] = toVideoResponse(&vs[i], channels[vs[i].BroadcasterID], primaryCategories[vs[i].ID])
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

type VideoListPageCursor struct {
	SortNumber      *float64  `json:"sort_number,omitempty"`
	SortInt         *string   `json:"sort_int,omitempty"`
	SortText        *string   `json:"sort_text,omitempty"`
	StartDownloadAt time.Time `json:"start_download_at" validate:"required"`
	ID              int64     `json:"id" validate:"required"`
}

type ListPageInput struct {
	Limit          int                  `json:"limit" validate:"min=0,max=200"`
	Status         string               `json:"status,omitempty" validate:"omitempty,oneof=PENDING RUNNING DONE FAILED"`
	Sort           string               `json:"sort,omitempty" validate:"omitempty,oneof=created_at duration size channel"`
	Order          string               `json:"order,omitempty" validate:"omitempty,oneof=asc desc"`
	Quality        string               `json:"quality,omitempty"`
	BroadcasterID  string               `json:"broadcaster_id,omitempty"`
	Language       string               `json:"language,omitempty"`
	Duration       string               `json:"duration,omitempty" validate:"omitempty,oneof=short medium long marathon"`
	Size           string               `json:"size,omitempty" validate:"omitempty,oneof=small medium large"`
	Window         string               `json:"window,omitempty" validate:"omitempty,oneof=this_week"`
	IncompleteOnly bool                 `json:"incomplete_only,omitempty"`
	Cursor         *VideoListPageCursor `json:"cursor,omitempty" validate:"omitempty"`
}

type VideoListPageResponse struct {
	Items      []VideoResponse      `json:"items"`
	NextCursor *VideoListPageCursor `json:"next_cursor,omitempty"`
}

func (h *Handler) ListPage(ctx context.Context, input ListPageInput) (VideoListPageResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	order := input.Order
	if input.Sort != "" && order == "" {
		order = "desc"
	}
	cursor, err := toRepositoryVideoListPageCursor(input.Cursor)
	if err != nil {
		return VideoListPageResponse{}, trpcgo.NewError(trpcgo.CodeBadRequest, "invalid video list cursor")
	}
	durationMin, durationMax := videoDurationFilterBounds(input.Duration)
	sizeMin, sizeMax := videoSizeFilterBounds(input.Size)
	page, err := h.video.ListPage(ctx, repository.ListVideosOpts{
		Status:             input.Status,
		Sort:               input.Sort,
		Order:              order,
		Quality:            input.Quality,
		BroadcasterID:      input.BroadcasterID,
		Language:           input.Language,
		DurationMinSeconds: durationMin,
		DurationMaxSeconds: durationMax,
		SizeMinBytes:       sizeMin,
		SizeMaxBytes:       sizeMax,
		Window:             input.Window,
		IncompleteOnly:     input.IncompleteOnly,
		Limit:              limit,
	}, cursor)
	if err != nil {
		h.log.Error("list video page", "error", err)
		return VideoListPageResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list videos")
	}
	return VideoListPageResponse{
		Items:      h.toVideoResponses(ctx, page.Items),
		NextCursor: toVideoListPageCursor(page.NextCursor),
	}, nil
}

func videoDurationFilterBounds(filter string) (*float64, *float64) {
	const minute = 60.0
	const hour = 60 * minute
	switch filter {
	case "short":
		return nil, float64Ptr(30 * minute)
	case "medium":
		return float64Ptr(30 * minute), float64Ptr(2 * hour)
	case "long":
		return float64Ptr(2 * hour), float64Ptr(4 * hour)
	case "marathon":
		return float64Ptr(4 * hour), nil
	default:
		return nil, nil
	}
}

func videoSizeFilterBounds(filter string) (*int64, *int64) {
	const gib = int64(1024 * 1024 * 1024)
	switch filter {
	case "small":
		return nil, int64Ptr(gib)
	case "medium":
		return int64Ptr(gib), int64Ptr(4 * gib)
	case "large":
		return int64Ptr(4 * gib), nil
	default:
		return nil, nil
	}
}

func float64Ptr(v float64) *float64 { return &v }

func int64Ptr(v int64) *int64 { return &v }

type GetByIDInput struct {
	ID int64 `json:"id" validate:"required"`
}

// TitleItem is the wire shape for one title in a video's history.
// ID is the deduplicated titles row; Name is the broadcast label.
// Listed in the order the titles were first linked to the video
// (opening title first, change events after).
type TitleItem struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	DurationSeconds float64    `json:"duration_seconds"`
}

type TitlesInput struct {
	VideoID int64 `json:"video_id" validate:"required"`
}

// VideoCategory is the wire shape for one category in a video's
// history. Parallels TitleItem — opening category first, mid-stream
// game switches after, distinct rows only.
type VideoCategory struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	BoxArtURL       *string    `json:"box_art_url,omitempty"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	DurationSeconds float64    `json:"duration_seconds"`
}

type CategoriesInput struct {
	VideoID int64 `json:"video_id" validate:"required"`
}

// VideoPageCursor identifies the last row from the previous page so the
// next query can continue with stable keyset pagination.
type VideoPageCursor struct {
	StartDownloadAt time.Time `json:"start_download_at" validate:"required"`
	ID              int64     `json:"id" validate:"required"`
}

// VideoPageResponse is the cursor-paginated envelope for channel/category
// detail video grids. next_cursor is omitted on the terminal page.
type VideoPageResponse struct {
	Items      []VideoResponse  `json:"items"`
	NextCursor *VideoPageCursor `json:"next_cursor,omitempty"`
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
		out[i] = TitleItem{ID: r.ID, Name: r.Name, StartedAt: r.StartedAt, EndedAt: r.EndedAt, DurationSeconds: r.DurationSeconds}
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
			ID:              r.ID,
			Name:            r.Name,
			BoxArtURL:       r.BoxArtURL,
			StartedAt:       r.StartedAt,
			EndedAt:         r.EndedAt,
			DurationSeconds: r.DurationSeconds,
		}
	}
	return out, nil
}

// TimelineTitle is the embedded title payload on a timeline event.
// Absent at the parent level when the originating channel.update
// did not carry a title.
type TimelineTitle struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// TimelineCategory is the embedded category payload on a timeline
// event. Absent when the originating event did not carry a category.
type TimelineCategory struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	BoxArtURL *string `json:"box_art_url,omitempty"`
}

// TimelineEvent is the wire shape for one merged title+category
// change row. The schema-level CHECK guarantees at least one of
// title/category is present.
type TimelineEvent struct {
	OccurredAt time.Time         `json:"occurred_at"`
	Title      *TimelineTitle    `json:"title,omitempty"`
	Category   *TimelineCategory `json:"category,omitempty"`
}

type TimelineInput struct {
	VideoID int64 `json:"video_id" validate:"required"`
}

// Timeline returns the merged chronological list of title and
// category change events for a video. Backed by
// video_metadata_changes; recordings predating migration 031 return
// empty and the dialog falls through to its empty-state copy.
func (h *Handler) Timeline(ctx context.Context, input TimelineInput) ([]TimelineEvent, error) {
	rows, err := h.video.Timeline(ctx, input.VideoID)
	if err != nil {
		h.log.Error("list video timeline", "video_id", input.VideoID, "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list timeline")
	}
	out := make([]TimelineEvent, len(rows))
	for i, r := range rows {
		event := TimelineEvent{OccurredAt: r.OccurredAt}
		if r.Title != nil {
			event.Title = &TimelineTitle{ID: r.Title.ID, Name: r.Title.Name}
		}
		if r.Category != nil {
			event.Category = &TimelineCategory{
				ID:        r.Category.ID,
				Name:      r.Category.Name,
				BoxArtURL: r.Category.BoxArtURL,
			}
		}
		out[i] = event
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
	primaryCategories := h.video.PrimaryCategoriesByVideoIDs(ctx, []repository.Video{*v})
	resp := toVideoResponse(v, channels[v.BroadcasterID], primaryCategories[v.ID])
	// Multi-part recordings expose their parts here so the player
	// can iterate them. A parts lookup failure is logged but doesn't
	// fail the whole getById — the player's fallback is to stream
	// part 01 by convention via the stream handler.
	if parts, err := h.video.Parts(ctx, v.ID); err != nil {
		h.log.Warn("list video parts", "video_id", v.ID, "error", err)
	} else {
		resp.Parts = toVideoPartResponses(parts)
	}
	return resp, nil
}

type ByBroadcasterInput struct {
	BroadcasterID string           `json:"broadcaster_id" validate:"required"`
	Limit         int              `json:"limit" validate:"min=0,max=200"`
	Cursor        *VideoPageCursor `json:"cursor,omitempty" validate:"omitempty"`
}

func (h *Handler) ByBroadcaster(ctx context.Context, input ByBroadcasterInput) (VideoPageResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	page, err := h.video.ListByBroadcaster(ctx, input.BroadcasterID, limit, toRepositoryVideoPageCursor(input.Cursor))
	if err != nil {
		h.log.Error("list videos by broadcaster", "error", err)
		return VideoPageResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list videos")
	}
	return VideoPageResponse{
		Items:      h.toVideoResponses(ctx, page.Items),
		NextCursor: toVideoPageCursor(page.NextCursor),
	}, nil
}

type ByCategoryInput struct {
	CategoryID string           `json:"category_id" validate:"required"`
	Limit      int              `json:"limit" validate:"min=0,max=200"`
	Cursor     *VideoPageCursor `json:"cursor,omitempty" validate:"omitempty"`
}

func (h *Handler) ByCategory(ctx context.Context, input ByCategoryInput) (VideoPageResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	page, err := h.video.ListByCategory(ctx, input.CategoryID, limit, toRepositoryVideoPageCursor(input.Cursor))
	if err != nil {
		h.log.Error("list videos by category", "error", err)
		return VideoPageResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list videos")
	}
	return VideoPageResponse{
		Items:      h.toVideoResponses(ctx, page.Items),
		NextCursor: toVideoPageCursor(page.NextCursor),
	}, nil
}

func toRepositoryVideoPageCursor(cursor *VideoPageCursor) *repository.VideoPageCursor {
	if cursor == nil {
		return nil
	}
	return &repository.VideoPageCursor{StartDownloadAt: cursor.StartDownloadAt, ID: cursor.ID}
}

func toVideoPageCursor(cursor *repository.VideoPageCursor) *VideoPageCursor {
	if cursor == nil {
		return nil
	}
	return &VideoPageCursor{StartDownloadAt: cursor.StartDownloadAt, ID: cursor.ID}
}

func toRepositoryVideoListPageCursor(cursor *VideoListPageCursor) (*repository.VideoListPageCursor, error) {
	if cursor == nil {
		return nil, nil
	}
	var sortInt *int64
	if cursor.SortInt != nil {
		parsed, err := strconv.ParseInt(*cursor.SortInt, 10, 64)
		if err != nil {
			return nil, err
		}
		sortInt = &parsed
	}
	return &repository.VideoListPageCursor{
		SortNumber:      cursor.SortNumber,
		SortInt:         sortInt,
		SortText:        cursor.SortText,
		StartDownloadAt: cursor.StartDownloadAt,
		ID:              cursor.ID,
	}, nil
}

func toVideoListPageCursor(cursor *repository.VideoListPageCursor) *VideoListPageCursor {
	if cursor == nil {
		return nil
	}
	out := &VideoListPageCursor{
		SortNumber:      cursor.SortNumber,
		SortText:        cursor.SortText,
		StartDownloadAt: cursor.StartDownloadAt,
		ID:              cursor.ID,
	}
	if cursor.SortInt != nil {
		s := strconv.FormatInt(*cursor.SortInt, 10)
		out.SortInt = &s
	}
	return out
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
	// ThisWeek and Incomplete drive the videos page tab counters.
	// Incomplete spans completion_kind='partial' OR truncated rows
	// — the same predicate as the Partial tab's server-side filter.
	ThisWeek   int64 `json:"this_week"`
	Incomplete int64 `json:"incomplete"`
	// Channels is the count of distinct broadcasters represented in
	// the videos table — used by the videos page subtitle.
	Channels int64 `json:"channels"`
}

type ActiveDownloadResponse struct {
	Video          VideoResponse `json:"video"`
	Stage          string        `json:"stage"`
	BytesWritten   int64         `json:"bytes_written"`
	SegmentsDone   int64         `json:"segments_done"`
	SegmentsGaps   int64         `json:"segments_gaps"`
	SegmentsAdGaps int64         `json:"segments_ad_gaps"`
	SegmentsTotal  int64         `json:"segments_total"`
	Percent        float64       `json:"percent"`
	Speed          string        `json:"speed,omitempty"`
	ETA            string        `json:"eta,omitempty"`
	RecordingType  string        `json:"recording_type,omitempty"`
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
		ThisWeek:      stats.Totals.ThisWeek,
		Incomplete:    stats.Totals.Incomplete,
		Channels:      stats.Totals.Channels,
	}
	for i, b := range stats.ByStatus {
		out.ByStatus[i] = StatsBucket{Status: b.Status, Count: b.Count}
	}
	return out, nil
}

// ChannelStatisticsInput scopes a per-channel aggregate query.
type ChannelStatisticsInput struct {
	BroadcasterID string `json:"broadcaster_id"`
}

// ChannelStatisticsResponse is the wire shape for video.statisticsByBroadcaster.
type ChannelStatisticsResponse struct {
	Total         int64   `json:"total"`
	TotalSize     int64   `json:"total_size"`
	TotalDuration float64 `json:"total_duration_seconds"`
}

func (h *Handler) StatisticsByBroadcaster(ctx context.Context, input ChannelStatisticsInput) (ChannelStatisticsResponse, error) {
	if input.BroadcasterID == "" {
		return ChannelStatisticsResponse{}, trpcgo.NewError(trpcgo.CodeBadRequest, "broadcaster_id is required")
	}
	totals, err := h.video.StatsByBroadcaster(ctx, input.BroadcasterID)
	if err != nil {
		h.log.Error("video statistics by broadcaster", "error", err, "broadcaster_id", input.BroadcasterID)
		return ChannelStatisticsResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load channel statistics")
	}
	return ChannelStatisticsResponse{
		Total:         totals.Total,
		TotalSize:     totals.TotalSize,
		TotalDuration: totals.TotalDuration,
	}, nil
}

func (h *Handler) ActiveDownloads(ctx context.Context) ([]ActiveDownloadResponse, error) {
	rows, err := h.activeDownloadsSnapshot(ctx)
	if err != nil {
		h.log.Error("list active downloads", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list active downloads")
	}
	return rows, nil
}

func (h *Handler) activeDownloadsSnapshot(ctx context.Context) ([]ActiveDownloadResponse, error) {
	progress := h.download.ActiveProgress()
	if len(progress) == 0 {
		return []ActiveDownloadResponse{}, nil
	}

	// Resolve video rows by the job IDs the downloader is actively
	// running. Using in-memory active jobs as the source of truth
	// avoids missing new downloads hidden behind any stale
	// RUNNING rows in the database (e.g. orphans from a prior crash).
	vids := make([]repository.Video, 0, len(progress))
	byJob := make(map[string]*repository.Video, len(progress))
	for _, snap := range progress {
		v, err := h.download.VideoByJobID(ctx, snap.JobID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				continue
			}
			return nil, err
		}
		vids = append(vids, *v)
		byJob[v.JobID] = &vids[len(vids)-1]
	}
	channels := h.video.ChannelsByBroadcasterIDs(ctx, vids)
	primaryCategories := h.video.PrimaryCategoriesByVideoIDs(ctx, vids)

	out := make([]ActiveDownloadResponse, 0, len(progress))
	for _, snap := range progress {
		v, ok := byJob[snap.JobID]
		if !ok {
			continue
		}
		resp := toVideoResponse(v, channels[v.BroadcasterID], primaryCategories[v.ID])
		if snap.Quality != "" {
			resp.Quality = formatQualityLabel(snap.Quality, snap.FPS)
			resp.FPS = snap.FPS
		}
		out = append(out, ActiveDownloadResponse{
			Video:          resp,
			Stage:          snap.Stage,
			BytesWritten:   snap.BytesWritten,
			SegmentsDone:   snap.SegmentsDone,
			SegmentsGaps:   snap.SegmentsGaps,
			SegmentsAdGaps: snap.SegmentsAdGaps,
			SegmentsTotal:  snap.SegmentsTotal,
			Percent:        snap.Percent,
			Speed:          snap.Speed,
			ETA:            snap.ETA,
			RecordingType:  snap.RecordingType,
		})
	}
	return out, nil
}

func (h *Handler) ActiveDownloadsLive(ctx context.Context) (<-chan []ActiveDownloadResponse, error) {
	src := h.download.SubscribeActive(ctx)
	out := make(chan []ActiveDownloadResponse, 8)

	go func() {
		defer close(out)

		sendSnapshot := func() bool {
			rows, err := h.activeDownloadsSnapshot(ctx)
			if err != nil {
				h.log.Error("stream active downloads", "error", err)
				return true
			}
			select {
			case out <- rows:
				return true
			case <-ctx.Done():
				return false
			}
		}

		if !sendSnapshot() {
			return
		}

		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-src:
				if !ok {
					return
				}
				if !sendSnapshot() {
					return
				}
			}
		}
	}()

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
	if user == nil {
		return TriggerDownloadResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	result, err := h.download.Trigger(ctx, TriggerInput{
		BroadcasterID: input.BroadcasterID,
		RecordingType: input.RecordingType,
		Quality:       input.Quality,
		ForceH264:     input.ForceH264,
		UserID:        user.ID,
	})
	if err != nil {
		if errors.Is(err, ErrChannelNotSynced) {
			return TriggerDownloadResponse{}, trpcgo.NewError(trpcgo.CodeNotFound,
				"channel not synced — run channel.syncFromTwitch first")
		}
		if twitch.IsUserAuthError(err) {
			h.log.Warn("trigger download", "error", err, "broadcaster_id", input.BroadcasterID)
			return TriggerDownloadResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized,
				"twitch session expired; sign in again")
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
	JobID          string   `json:"job_id"`
	PartIndex      int      `json:"part_index"`
	Stage          string   `json:"stage"`
	BytesWritten   int64    `json:"bytes_written"`
	SegmentsDone   int64    `json:"segments_done"`
	SegmentsGaps   int64    `json:"segments_gaps"`
	SegmentsAdGaps int64    `json:"segments_ad_gaps"`
	SegmentsTotal  int64    `json:"segments_total"`
	Percent        float64  `json:"percent"`
	Speed          string   `json:"speed,omitempty"`
	ETA            string   `json:"eta,omitempty"`
	Quality        string   `json:"quality,omitempty"`
	FPS            *float64 `json:"fps,omitempty"`
	Codec          string   `json:"codec,omitempty"`
	RecordingType  string   `json:"recording_type,omitempty"`
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
					Quality:        formatQualityLabel(p.Quality, p.FPS),
					FPS:            p.FPS,
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
