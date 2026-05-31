package recordingwebhook

import (
	"context"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

// PayloadVersion is the schema version stamped into every delivery's `version`
// field. Bump it on any breaking change to the wire shape so a receiver can
// branch on it instead of guessing.
const PayloadVersion = 1

// Payload is the JSON body of an outbound delivery. It is a self-contained
// snapshot of the recording at terminal time so a receiver can act without
// calling back into the API — and each part carries a signed download URL for
// receivers that want the file bytes.
type Payload struct {
	// Version is the payload schema version (see PayloadVersion).
	Version int `json:"version"`
	// Event is the terminal event identifier (recording.completed / recording.failed).
	Event string `json:"event"`
	// Test is true only for a dashboard "send test" delivery (event
	// recording.test), so a receiver can recognize and ignore a probe. Omitted
	// (false) on every real recording delivery.
	Test bool `json:"test,omitempty"`
	// VideoID is the ReplayVOD video row id; each part's DownloadURL embeds it.
	VideoID int64 `json:"video_id"`
	// Status is the videos.status value (DONE / FAILED).
	Status string `json:"status"`
	// CompletionKind distinguishes content-completeness (complete / partial /
	// cancelled); Truncated is the orthogonal "stopped before the broadcast
	// ended" axis. Both mirror the video row exactly.
	CompletionKind string `json:"completion_kind"`
	Truncated      bool   `json:"truncated"`

	BroadcasterID    string `json:"broadcaster_id"`
	BroadcasterLogin string `json:"broadcaster_login,omitempty"`
	BroadcasterName  string `json:"broadcaster_name,omitempty"`

	Title    string `json:"title,omitempty"`
	Category string `json:"category,omitempty"`

	// StartedAt is when the recording began (videos.start_download_at); EndedAt
	// is when it finalized (videos.downloaded_at), absent for a failure that
	// never finalized.
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`

	DurationSeconds *float64 `json:"duration_seconds,omitempty"`
	// TotalSizeBytes is the aggregate recorded size across all parts.
	TotalSizeBytes *int64 `json:"total_size_bytes,omitempty"`

	// Error carries the failure reason for recording.failed deliveries.
	Error *string `json:"error,omitempty"`

	// Parts lists each recorded segment file, in order. Every part carries its
	// own signed download URL (see PayloadPart.DownloadURL), so a multi-part
	// recording exposes all of its files, not just the first.
	Parts []PayloadPart `json:"parts"`
}

// PayloadPart describes one recorded part file.
type PayloadPart struct {
	PartIndex int32 `json:"part_index"`
	// Path is the storage-relative path (e.g. videos/<filename>), the same key
	// the storage backend and the streaming endpoint resolve against. Useful to
	// a consumer that shares the storage volume (a co-located sidecar).
	Path            string  `json:"path"`
	SizeBytes       int64   `json:"size_bytes"`
	DurationSeconds float64 `json:"duration_seconds"`
	// DownloadURL is an absolute, signed, expiring URL that streams THIS part's
	// bytes over plain HTTP with no session required, for a remote consumer.
	// Empty when signed URLs are disabled or no public origin is resolvable, in
	// which case a consumer falls back to Path. The URL's lifetime is the
	// operator's Download.SignedURLTTLHours, capped by the recording's retention
	// deadline when recording auto-delete is enabled and a retention policy applies.
	DownloadURL string `json:"download_url,omitempty"`
}

// payloadStore is the narrow slice of repository.Repository buildPayload needs.
// Kept small so payload assembly is exercisable with a fake.
type payloadStore interface {
	GetVideo(ctx context.Context, id int64) (*repository.Video, error)
	GetChannel(ctx context.Context, broadcasterID string) (*repository.Channel, error)
	ListVideoParts(ctx context.Context, videoID int64) ([]repository.VideoPart, error)
	ListPrimaryCategoriesForVideos(ctx context.Context, videoIDs []int64) (map[int64]repository.Category, error)
}

// partURLSigner mints a signed, expiring, unauthenticated download URL for one
// recorded part, capped at notAfter when provided, or "" when signed URLs are
// disabled or already expired. It is the videodownload Signer's PartURLUntil,
// injected as a function so this package stays off the videodownload import.
type partURLSigner func(videoID int64, partIndex int32, notAfter *time.Time) string

// buildPayload assembles the delivery body for one terminal event. The video
// and its parts are load-bearing (a delivery without them is meaningless, so
// their errors propagate and the delivery is abandoned); the channel and
// category are decorative and resolved best-effort.
//
// Signed download URLs are stamped onto parts only for recording.completed while
// the video is still visible: the signed-download route serves only DONE,
// non-deleted videos, so a download_url on a recording.failed or retained
// payload would be immediately broken. A failed or retained payload still lists
// its parts (paths, sizes) without a download_url.
func buildPayload(ctx context.Context, store payloadStore, signURL partURLSigner, eventID string, videoID int64, frozenParts []PayloadPart, capDownloadURLsAtRetention bool) (*Payload, error) {
	video, err := store.GetVideo(ctx, videoID)
	if err != nil {
		return nil, fmt.Errorf("load video %d: %w", videoID, err)
	}

	// Only a completed, non-retained recording has a servable file behind its
	// signed URL. When a retention policy applies, cap the URL expiry to the
	// recording's deletion deadline so the URL does not outlive the bytes.
	partSigner := signURL
	var downloadURLNotAfter *time.Time
	if capDownloadURLsAtRetention {
		downloadURLNotAfter = retentionDownloadDeadline(video)
	}
	if eventID != EventCompleted || video.DeletedAt != nil {
		partSigner = nil
	}

	// frozenParts is the snapshot captured on the first attempt, before retention
	// could delete the parts. When present, rebuild from it and mint fresh URLs;
	// otherwise read the live parts. Video/channel/category always come from the
	// current rows; retention tombstones the video row and deletes only its parts.
	var parts []PayloadPart
	if frozenParts != nil {
		parts = stampPartURLs(video.ID, frozenParts, partSigner, downloadURLNotAfter)
	} else {
		raw, err := store.ListVideoParts(ctx, videoID)
		if err != nil {
			return nil, fmt.Errorf("load parts for video %d: %w", videoID, err)
		}
		parts = partsPayload(video.ID, raw, partSigner, downloadURLNotAfter)
	}

	p := &Payload{
		Version:         PayloadVersion,
		Event:           eventID,
		VideoID:         video.ID,
		Status:          video.Status,
		CompletionKind:  video.CompletionKind,
		Truncated:       video.Truncated,
		BroadcasterID:   video.BroadcasterID,
		Title:           video.Title,
		StartedAt:       video.StartDownloadAt,
		EndedAt:         video.DownloadedAt,
		DurationSeconds: video.DurationSeconds,
		TotalSizeBytes:  video.SizeBytes,
		Error:           video.Error,
		Parts:           parts,
	}

	// Channel + category are decorative: a missing local mirror row must not
	// abandon a delivery, so both are best-effort.
	if ch, err := store.GetChannel(ctx, video.BroadcasterID); err == nil && ch != nil {
		p.BroadcasterLogin = ch.BroadcasterLogin
		p.BroadcasterName = ch.BroadcasterName
	}
	if cats, err := store.ListPrimaryCategoriesForVideos(ctx, []int64{videoID}); err == nil {
		if c, ok := cats[videoID]; ok {
			p.Category = c.Name
		}
	}
	return p, nil
}

// partsPayload maps repository parts to the wire shape, preserving order, and
// stamps each with a signed download URL when signURL is provided.
func partsPayload(videoID int64, parts []repository.VideoPart, signURL partURLSigner, notAfter *time.Time) []PayloadPart {
	out := make([]PayloadPart, len(parts))
	for i, part := range parts {
		pp := PayloadPart{
			PartIndex:       part.PartIndex,
			Path:            storagekeys.Video(part.Filename),
			SizeBytes:       part.SizeBytes,
			DurationSeconds: part.DurationSeconds,
		}
		if signURL != nil {
			pp.DownloadURL = signURL(videoID, part.PartIndex, notAfter)
		}
		out[i] = pp
	}
	return out
}

// stampPartURLs returns a copy of the frozen parts with a freshly minted signed
// download URL on each (or none when signURL is nil). Only the time-limited URL
// is regenerated; the frozen metadata (path, size, index, duration) is immutable,
// so a late retry ships a current URL rather than a stale one frozen at enqueue.
func stampPartURLs(videoID int64, parts []PayloadPart, signURL partURLSigner, notAfter *time.Time) []PayloadPart {
	out := make([]PayloadPart, len(parts))
	for i, pp := range parts {
		pp.DownloadURL = ""
		if signURL != nil {
			pp.DownloadURL = signURL(videoID, pp.PartIndex, notAfter)
		}
		out[i] = pp
	}
	return out
}

func retentionDownloadDeadline(video *repository.Video) *time.Time {
	if video == nil || video.RetentionWindowHours == nil || video.DownloadedAt == nil {
		return nil
	}
	hours := *video.RetentionWindowHours
	if hours <= 0 || hours > repository.MaxRetentionWindowHours {
		alreadyExpired := time.Unix(0, 0).UTC()
		return &alreadyExpired
	}
	deadline := video.DownloadedAt.Add(time.Duration(hours) * time.Hour)
	return &deadline
}

// stripPartURLs returns a copy with download URLs cleared: the shape frozen onto
// the delivery row. URLs are re-minted per attempt (stampPartURLs), never stored.
func stripPartURLs(parts []PayloadPart) []PayloadPart {
	out := make([]PayloadPart, len(parts))
	for i, pp := range parts {
		pp.DownloadURL = ""
		out[i] = pp
	}
	return out
}
