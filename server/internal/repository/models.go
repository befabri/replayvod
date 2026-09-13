package repository

import (
	"encoding/json"
	"time"
)

type User struct {
	ID              string
	Login           string
	DisplayName     string
	Email           *string
	ProfileImageURL *string
	Role            string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Session stores a SHA-256 hash of the session ID; the browser cookie holds
// the raw ID, which must never be persisted.
type Session struct {
	HashedID        string
	UserID          string
	EncryptedTokens []byte
	ExpiresAt       time.Time
	LastActiveAt    time.Time
	UserAgent       *string
	IPAddress       *string
	CreatedAt       time.Time
}

type SessionInfo struct {
	HashedID     string
	UserID       string
	ExpiresAt    time.Time
	LastActiveAt time.Time
	UserAgent    *string
	IPAddress    *string
	CreatedAt    time.Time
}

type AppAccessToken struct {
	ID        int64
	Token     string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// TwitchPlaybackSession is the owner-managed website credential for the shared
// recorder. It must never be returned by an API; only status/account metadata is
// public to the owner. Timestamps are Unix seconds; ExpiresAt may be unknown (0).
type TwitchPlaybackSession struct {
	TwitchUserID   string
	TwitchLogin    string
	EncryptedToken []byte
	ExpiresAt      int64
	CheckedAt      int64
	NeedsReconnect bool
}

type WhitelistEntry struct {
	TwitchUserID string
	AddedAt      time.Time
}

// Invite stores a single-use invitation with a SHA-256 token hash. Redeemed
// invitations remain as audit records.
type Invite struct {
	ID         int64
	TokenHash  string
	Role       string
	Note       *string
	CreatedBy  string
	ExpiresAt  time.Time
	RedeemedAt *time.Time
	RedeemedBy *string
	CreatedAt  time.Time
}

type InviteInput struct {
	TokenHash string
	Role      string
	Note      *string
	CreatedBy string
	ExpiresAt time.Time
}

// Schedule request states; APPROVED and REJECTED are terminal.
const (
	ScheduleRequestStatusPending  = "PENDING"
	ScheduleRequestStatusApproved = "APPROVED"
	ScheduleRequestStatusRejected = "REJECTED"
)

// ScheduleRequest records a request to automatically record a channel.
type ScheduleRequest struct {
	ID            int64
	BroadcasterID string
	RequestedBy   string
	Note          *string
	Status        string
	DecidedBy     *string
	DecidedAt     *time.Time
	ScheduleID    *int64
	CreatedAt     time.Time
}

// ScheduleRequestView includes the channel and requester display metadata.
type ScheduleRequestView struct {
	ScheduleRequest
	BroadcasterLogin string
	BroadcasterName  string
	ProfileImageURL  *string
	RequestedByLogin string
	RequestedByName  string
}

type Channel struct {
	BroadcasterID       string
	BroadcasterLogin    string
	BroadcasterName     string
	BroadcasterLanguage *string
	ProfileImageURL     *string
	OfflineImageURL     *string
	Description         *string
	BroadcasterType     *string
	ViewCount           int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

const (
	ChannelFilterAll        = "all"
	ChannelFilterLive       = "live"
	ChannelFilterDownloaded = "downloaded"
	ChannelFilterFavorites  = "favorites"
)

// ChannelPageCursor is the stable keyset cursor for channel lists.
// broadcaster_name is compared case-insensitively; broadcaster_id breaks ties.
type ChannelPageCursor struct {
	BroadcasterName string
	BroadcasterID   string
}

type ChannelPage struct {
	Items      []Channel
	NextCursor *ChannelPageCursor
}

// ChannelUserState is one user's library state for one channel.
// No row means the default state, including not favorited.
type ChannelUserState struct {
	UserID        string
	BroadcasterID string
	Favorite      bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type UserFollow struct {
	UserID        string
	BroadcasterID string
	FollowedAt    time.Time
	Followed      bool
}

type Category struct {
	ID                    string
	Name                  string
	BoxArtURL             *string
	IGDBID                *string
	Description           *string
	GameMetadataCheckedAt *time.Time
	DescriptionCheckedAt  *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// CategoryDetail combines Twitch category metadata with visible recording totals.
type CategoryDetail struct {
	Category   Category
	VideoCount int64
	TotalSize  int64
}

// CategoryPageCursor is the stable keyset cursor for the category browse list.
// Name and ID are always used as deterministic tie-breakers. LatestVideoAt and
// VideoCount are populated only for the matching sort modes.
type CategoryPageCursor struct {
	Name          string
	ID            string
	LatestVideoAt *time.Time
	VideoCount    int64
}

// CategoryPageItem supplies the aggregate sort values needed to build a cursor.
type CategoryPageItem struct {
	Category      Category
	LatestVideoAt time.Time
	VideoCount    int64
}

type CategoryPage struct {
	Items      []Category
	NextCursor *CategoryPageCursor
}

// UniqueCategoriesByID keeps the first row for each ID so a batch upsert cannot
// violate PostgreSQL's restriction on touching the same row twice.
func UniqueCategoriesByID(categories []Category) []Category {
	if len(categories) == 0 {
		return []Category{}
	}
	out := make([]Category, 0, len(categories))
	seen := make(map[string]struct{}, len(categories))
	for _, c := range categories {
		if _, ok := seen[c.ID]; ok {
			continue
		}
		seen[c.ID] = struct{}{}
		out = append(out, c)
	}
	return out
}

// OrderCategoriesByIDs returns the rows found in rows ordered by ids. Missing
// IDs are skipped and duplicate IDs are returned once at their first position.
func OrderCategoriesByIDs(rows []Category, ids []string) []Category {
	if len(rows) == 0 || len(ids) == 0 {
		return []Category{}
	}
	byID := make(map[string]Category, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	out := make([]Category, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		row, ok := byID[id]
		if !ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, row)
	}
	return out
}

// CategorySearchCache stores Twitch's result order without duplicating category
// metadata; the search service reorders resolved rows by local relevance.
type CategorySearchCache struct {
	NormalizedQuery string
	CategoryIDs     []string
	ExpiresAt       time.Time
	LastAccessedAt  time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type CategorySearchCacheInput struct {
	NormalizedQuery string
	CategoryIDs     []string
	ExpiresAt       time.Time
	LastAccessedAt  time.Time
}

type Tag struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

type FetchLog struct {
	ID            int64
	UserID        *string
	FetchType     string
	BroadcasterID *string
	Status        int
	Error         *string
	DurationMs    int64
	FetchedAt     time.Time
}

type FetchLogInput struct {
	UserID        *string
	FetchType     string
	BroadcasterID *string
	Status        int
	Error         *string
	DurationMs    int64
}

// Video status/quality enums. Stored as TEXT with a CHECK constraint so both
// dialects enforce the same value set without a server-side enum type.
const (
	VideoStatusPending = "PENDING"
	VideoStatusRunning = "RUNNING"
	VideoStatusDone    = "DONE"
	VideoStatusFailed  = "FAILED"

	QualityLow    = "LOW"
	QualityMedium = "MEDIUM"
	QualityHigh   = "HIGH"
	Quality1440   = "1440"
	QualityBest   = "BEST"
)

// QualityTierForHeight maps rendition height to the stored quality tier.
func QualityTierForHeight(height int) string {
	switch {
	case height <= 480:
		return QualityLow
	case height <= 720:
		return QualityMedium
	case height <= 1080:
		return QualityHigh
	case height <= 1440:
		return Quality1440
	default:
		return QualityBest
	}
}

type Stream struct {
	ID            string
	BroadcasterID string
	Type          string
	Language      string
	ThumbnailURL  *string
	ViewerCount   int64
	IsMature      *bool
	StartedAt     time.Time
	EndedAt       *time.Time
	CreatedAt     time.Time
}

// LatestLiveStream includes channel display metadata for the latest broadcast.
type LatestLiveStream struct {
	Stream
	BroadcasterLogin string
	BroadcasterName  string
	ProfileImageURL  *string
}

type StreamInput struct {
	ID            string
	BroadcasterID string
	Type          string
	Language      string
	ThumbnailURL  *string
	ViewerCount   int64
	IsMature      *bool
	StartedAt     time.Time
}

// Video is a recording and its lifecycle state. ForceH264 excludes HEVC and AV1
// video renditions; audio recordings ignore it.
type Video struct {
	// LastProgressAtMs holds Unix milliseconds for last_watched pages; nil means
	// no saved progress or a query using another sort.
	LastProgressAtMs *int64
	ID               int64
	JobID            string
	Filename         string
	DisplayName      string
	// Title is the broadcast title observed at admission, or empty when unknown.
	Title  string
	Status string
	// Quality is the requested tier; SelectedQuality and SelectedFPS identify the
	// recorded rendition.
	Quality         string
	SelectedQuality *string
	SelectedFPS     *float64
	BroadcasterID   string
	StreamID        *string
	ViewerCount     int64
	Language        string
	DurationSeconds *float64
	SizeBytes       *int64
	Thumbnail       *string
	Error           *string
	StartDownloadAt time.Time
	DownloadedAt    *time.Time
	DeletedAt       *time.Time
	// DeleteRequestedAt marks a live recording queued for operator-requested
	// background deletion. DeletedAt remains nil until the worker purges storage
	// and finalizes the tombstone.
	DeleteRequestedAt *time.Time
	// DeletionKind records why a tombstoned recording was removed:
	// "retention" (auto-pruned by the schedule retention sweep), "manual" (an
	// operator removed it) or "missing" (its media was gone from storage). Nil
	// while DeletedAt is nil.
	DeletionKind  *string
	RecordingType string
	ForceH264     bool
	// TriggerScheduleID is the highest-quality schedule that caused this
	// recording. RetentionSourceScheduleID is the matched delete-enabled
	// schedule whose shortest window was snapshotted onto the recording.
	// Both are nil for manual recordings.
	TriggerScheduleID         *int64
	RetentionSourceScheduleID *int64
	RetentionWindowHours      *int64
	// CompletionKind records content completeness independently of pipeline success:
	// complete, partial, or cancelled by the operator.
	CompletionKind string
	// Truncated reports that capture stopped before the broadcast ended, regardless
	// of whether the captured portion is complete or partial.
	Truncated bool
	// Source is "live" for a recording captured from a broadcast and "vod"
	// for an archive of a Twitch VOD downloaded after the fact. TwitchVideoID
	// and BroadcastAt are set only for archives: the VOD id on Twitch and the
	// date the stream originally aired.
	Source        string
	TwitchVideoID *string
	BroadcastAt   *time.Time
	// NextRetryAt is set on a failed archive whose next attempt is scheduled.
	// The row stays open under the one-row-per-VOD rule until the retry runs
	// or is cancelled. Always nil for live recordings.
	NextRetryAt *time.Time
}

// VideoSource enumerates videos.source.
const (
	VideoSourceLive = "live"
	VideoSourceVOD  = "vod"
)

// VideoSourceOrLive applies the creation-input default shared by both adapters.
// Persisted rows already have a constrained source and must be read as stored.
func VideoSourceOrLive(source string) string {
	if source == "" {
		return VideoSourceLive
	}
	return source
}

// Recording completion kinds are independent of pipeline success.
const (
	CompletionKindComplete  = "complete"
	CompletionKindPartial   = "partial"
	CompletionKindCancelled = "cancelled"
)

// Recording outcomes combine terminal status with operator cancellation; they
// are not stored separately.
const (
	VideoOutcomeCompleted = "completed"
	VideoOutcomeFailed    = "failed"
	VideoOutcomeCancelled = "cancelled"
)

// ClassifyVideoOutcome folds a terminal row's status and completion kind into
// one VideoOutcome. Keep in lockstep with the Outcome predicate in
// videos_page_sql.go, which expresses the same rule as SQL.
func ClassifyVideoOutcome(status, completionKind string) string {
	if status != VideoStatusFailed {
		return VideoOutcomeCompleted
	}
	if completionKind == CompletionKindCancelled {
		return VideoOutcomeCancelled
	}
	return VideoOutcomeFailed
}

// Recording deletion kinds identify why a row was tombstoned.
const (
	DeletionKindRetention = "retention"
	DeletionKindManual    = "manual"
	// DeletionKindMissing marks media that left storage behind ReplayVOD's back;
	// the storage scan or a playback 404 tombstoned the row.
	DeletionKindMissing = "missing"
)

// MaxRetentionWindowHours is the largest time_before_delete value that can be
// converted to a time.Duration without overflowing nanoseconds.
const MaxRetentionWindowHours int64 = int64(1<<63-1) / int64(time.Hour)

// VideoInput admits a recording; an empty RecordingType defaults to video.
type VideoInput struct {
	StreamStartedAt     time.Time // observed broadcast start; zero when unknown
	IntentID            string
	IntentPreviousJobID string
	IntentParams        json.RawMessage
	RestartWaitSeconds  int64
	IntentObservedAt    time.Time
	JobID               string
	Filename            string
	DisplayName         string
	Title               string
	Status              string
	Quality             string
	BroadcasterID       string
	StreamID            *string
	ViewerCount         int64
	Language            string
	RecordingType       string
	ForceH264           bool
	// TriggerScheduleID and retention fields are nil for manual recordings, which
	// are exempt from automatic retention.
	TriggerScheduleID         *int64
	RetentionSourceScheduleID *int64
	RetentionWindowHours      *int64
	// Source empty means live. Archives set VideoSourceVOD plus the VOD id and
	// the original broadcast date.
	Source        string
	TwitchVideoID *string
	BroadcastAt   *time.Time
}

// RecordingType enumerates the two recording modes. Stored on
// videos.recording_type with a CHECK constraint matching these values.
const (
	RecordingTypeVideo = "video"
	RecordingTypeAudio = "audio"
)

// NormalizeRecordingType returns audio for an audio request and video otherwise.
func NormalizeRecordingType(value string) string {
	if value == RecordingTypeAudio {
		return RecordingTypeAudio
	}
	return RecordingTypeVideo
}

// ScheduleForceH264 reports whether H.264 is requested for a video recording;
// audio always returns false.
func ScheduleForceH264(recordingType string, forceH264 bool) bool {
	return NormalizeRecordingType(recordingType) == RecordingTypeVideo && forceH264
}

// RecordingSettings is the normalized recording-mode payload shared by schedule
// writes, schedule dispatch, and video row creation.
type RecordingSettings struct {
	RecordingType string
	Quality       string
	ForceH264     bool
}

// RecordingSettingsInput carries caller-provided recording options before
// defaults and video-only rules are applied.
type RecordingSettingsInput struct {
	RecordingType string
	Quality       string
	ForceH264     bool
}

// NormalizeRecordingSettings defaults unknown recording types to video and empty
// quality to HIGH, and clears ForceH264 for audio.
func NormalizeRecordingSettings(input RecordingSettingsInput) RecordingSettings {
	recordingType := NormalizeRecordingType(input.RecordingType)
	quality := input.Quality
	if quality == "" {
		quality = QualityHigh
	}
	return RecordingSettings{
		RecordingType: recordingType,
		Quality:       quality,
		ForceH264:     ScheduleForceH264(recordingType, input.ForceH264),
	}
}

// Codec enumerates the codec values stored on video_parts.codec.
// 'aac' is the audio-only mode; video modes use h264/h265/av1.
const (
	CodecH264 = "h264"
	CodecH265 = "h265"
	CodecAV1  = "av1"
	CodecAAC  = "aac"
)

// SegmentFormat enumerates the fragment container values stored on
// video_parts.segment_format. Sticky within a part.
const (
	SegmentFormatTS   = "ts"
	SegmentFormatFMP4 = "fmp4"
)

// JobStatus enumerates the durable job state machine. Stored on jobs.status
// with a CHECK constraint matching these values.
const (
	JobStatusPending = "PENDING"
	JobStatusRunning = "RUNNING"
	JobStatusDone    = "DONE"
	JobStatusFailed  = "FAILED"
)

// Job records a recording attempt and its recovery checkpoint; archive retries
// create additional jobs for the same video.
type Job struct {
	StopRequested   bool
	ExecutionID     string
	AcceptsMetadata bool
	ID              string
	VideoID         int64
	BroadcasterID   string
	Status          string
	// Attempt numbers this job among the attempts of its video, starting at 1.
	// Archive retries create a new job per attempt.
	Attempt     int32
	StartedAt   *time.Time
	FinishedAt  *time.Time
	Error       *string
	ResumeState json.RawMessage
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Recording intent states retain the manual request while active or waiting.
const (
	RecordingIntentStatusActive  = "active"
	RecordingIntentStatusWaiting = "waiting"
	RecordingIntentStatusStopped = "stopped"
	RecordingIntentStatusExpired = "expired"
)

// RecordingIntent holds one manual request across successive broadcasts until
// its restart window expires or the operator stops it.
type RecordingIntent struct {
	ID            string
	BroadcasterID string
	Params        json.RawMessage
	WaitSeconds   int64
	Status        string
	CurrentJobID  string
	LastStreamID  string
	WaitUntil     *time.Time
	StopRequested bool
	CreatedAt     time.Time
}

// RelatedRecording locates a video within a manual request's ordered history,
// including removed recordings.
type RelatedRecording struct {
	ID              int64
	JobID           string
	Title           string
	Status          string
	CompletionKind  string
	DeletedAt       *time.Time
	StartDownloadAt time.Time
	Position        int64
}

// MediaPublication tracks an object independently of application rows so cleanup
// can reconcile an upload that finishes after its recording was deleted.
type MediaPublication struct {
	Key             string
	VideoID         int64
	Digest          string
	SizeBytes       int64
	Unresolved      bool
	DeleteRequested bool
}

// JobInput creates a PENDING job; an empty ResumeState is stored as {}.
type JobInput struct {
	ID            string
	VideoID       int64
	BroadcasterID string
	ResumeState   json.RawMessage
	// Attempt is 1-based; zero means the first attempt.
	Attempt int32
}

// VideoPart records one output file, ordered by PartIndex. A nil EndMediaSeq
// means finalization has not committed; zero is a valid finalized sequence.
type VideoPart struct {
	ID              int64
	VideoID         int64
	PartIndex       int32
	Filename        string
	Quality         string
	FPS             *float64
	Codec           string
	SegmentFormat   string
	DurationSeconds float64
	SizeBytes       int64
	Thumbnail       *string
	StartMediaSeq   int64
	EndMediaSeq     *int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// VideoPartInput creates a part before its duration, size, and final sequence
// are known.
type VideoPartInput struct {
	VideoID       int64
	PartIndex     int32
	Filename      string
	Quality       string
	FPS           *float64
	Codec         string
	SegmentFormat string
	StartMediaSeq int64
}

// VideoPartFinalize commits the measured output and requires EndMediaSeq,
// including zero when that is the actual final sequence.
type VideoPartFinalize struct {
	ID              int64
	DurationSeconds float64
	SizeBytes       int64
	Thumbnail       *string
	EndMediaSeq     int64
}

const (
	PlaybackAssetStatusBuilding = "building"
	PlaybackAssetStatusReady    = "ready"
	// PlaybackAssetStatusFailed permits retries; PlaybackAssetStatusUnavailable
	// requires different source media or a larger capacity limit.
	PlaybackAssetStatusFailed      = "failed"
	PlaybackAssetStatusUnavailable = "unavailable"
)

// VideoPlaybackAsset describes a derived playback file; original video parts
// remain the durable recording outputs.
type VideoPlaybackAsset struct {
	VideoID         int64
	Status          string
	Filename        *string
	MimeType        *string
	DurationSeconds *float64
	SizeBytes       *int64
	Error           *string
	GeneratedAt     *time.Time
	LastAccessedAt  *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// VideoPlaybackAssetInput requires all media fields for ready assets; other
// statuses require nil Filename, MimeType, and LastAccessedAt.
type VideoPlaybackAssetInput struct {
	VideoID         int64
	Status          string
	Filename        *string
	MimeType        *string
	DurationSeconds *float64
	SizeBytes       *int64
	Error           *string
	GeneratedAt     *time.Time
	LastAccessedAt  *time.Time
}

type Title struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

// TitleSpan is one concrete interval during which a video carried a title.
// A title that appears, changes away, then appears again produces two rows.
type TitleSpan struct {
	Title
	StartedAt       time.Time
	EndedAt         *time.Time
	DurationSeconds float64
}

type CategorySpan struct {
	Category
	StartedAt       time.Time
	EndedAt         *time.Time
	DurationSeconds float64
}

// VideoMetadataChange records observed title and category values at OccurredAt;
// a nil dimension was absent from the observation, and both cannot be nil.
type VideoMetadataChange struct {
	ID                 int64
	VideoID            int64
	OccurredAt         time.Time
	MediaOffsetSeconds *float64
	Title              *Title
	Category           *Category
}

// VideoMetadataChangeInput carries an observation for the owning execution;
// empty Title and CategoryID mean unobserved dimensions, and empty CategoryName
// preserves the stored category name.
type VideoMetadataChangeInput struct {
	JobID              string
	ExecutionID        string
	Initial            bool
	VideoID            int64
	OccurredAt         time.Time
	MediaOffsetSeconds *float64
	Title              string
	CategoryID         string
	CategoryName       string
}

// VideoMetadataChangeResult returns the stored metadata for effects performed
// after the observation commits.
type VideoMetadataChangeResult struct {
	Title    *Title
	Category *Category
}

// VideoStatsTotals is the aggregate row for video.statistics.
// Total/TotalSize/TotalDuration are DONE-only rollups; Incomplete counts
// partial or truncated recordings.
type VideoStatsTotals struct {
	Total         int64
	TotalSize     int64
	TotalDuration float64
	ThisWeek      int64
	Incomplete    int64
	Channels      int64
	// Removed counts tombstoned recordings (deleted_at IS NOT NULL). Zero for
	// the per-broadcaster rollup, which only aggregates live DONE rows.
	Removed          int64
	WatchLater       int64
	Unwatched        int64
	ContinueWatching int64
}

type VideoStatsByStatus struct {
	Status string
	Count  int64
}

// VideoStatsHistoryBucket counts terminal recordings by status, completeness,
// and deletion state; callers derive outcomes with ClassifyVideoOutcome.
type VideoStatsHistoryBucket struct {
	Status         string
	CompletionKind string
	Removed        bool
	// DeletionKind is empty for live rows and names why a tombstone left.
	DeletionKind string
	Count        int64
}

// RetentionVideo identifies reclaimable terminal media under a captured retention
// policy; queries guarantee non-nil DownloadedAt and RetentionWindowHours.
type RetentionVideo struct {
	VideoID              int64
	BroadcasterID        string
	DownloadedAt         *time.Time
	RetentionWindowHours *int64
}

// StorageScanVideo identifies a terminal recording eligible for a media check.
type StorageScanVideo struct {
	VideoID  int64
	Filename string
	Status   string
}

// ListVideosOpts filters and orders recording queries; empty Status includes
// every status, and unknown sorts default to created_at descending. Page queries
// place null size, duration, and last-watched values last in either direction.
type ListVideosOpts struct {
	// UserID scopes per-user library filters such as watch later and unwatched.
	// Empty is valid for global surfaces but makes those per-user filters match
	// nothing.
	UserID string
	Status string // "" | "PENDING" | "RUNNING" | "DONE" | "FAILED"
	Sort   string // "" | "created_at" | "duration" | "size" | "channel" | "history_when" | "broadcast_at" | "last_watched"
	// Source narrows to live recordings or archives; "" returns both.
	Source             string // "" | "live" | "vod"
	Order              string // "" | "asc" | "desc"
	Quality            string
	BroadcasterID      string
	Language           string
	DurationMinSeconds *float64
	DurationMaxSeconds *float64
	SizeMinBytes       *int64
	SizeMaxBytes       *int64
	// Window applies a recency filter to StartDownloadAt; empty disables it.
	Window string // "" | "this_week"
	// IncompleteOnly narrows to recordings that did not capture the full
	// broadcast: completion_kind='partial' OR truncated.
	IncompleteOnly bool
	WatchLaterOnly bool
	// UnwatchedOnly narrows to playable recordings the current user has not
	// started watching yet. A watch-later-only row with no watched_at timestamp
	// still counts as unwatched.
	UnwatchedOnly bool
	// ContinueWatchingOnly lists started recordings with a resumable position.
	ContinueWatchingOnly bool
	// Outcome filters terminal status and completion kind using ClassifyVideoOutcome;
	// empty includes every outcome.
	Outcome string // "" | "completed" | "failed" | "cancelled"
	// TerminalOnly includes DONE and FAILED recordings.
	TerminalOnly bool
	// Scope selects active, removed, or all rows in ListVideosPage only; empty
	// means active, and other listing methods always exclude tombstones.
	Scope string // "" | "active" | "removed" | "all"
	// DeletionKind narrows tombstones to why they left ("retention", "manual",
	// "missing"); "" keeps every kind.
	DeletionKind string
	Limit        int
	Offset       int
}

// VideoUserState is the current user's library state for one video. Rows are
// sparse: no row means not saved for later and no playback progress.
type VideoUserState struct {
	UserID              string
	VideoID             int64
	WatchLater          bool
	LastPositionSeconds float64
	LastProgressAtMs    *int64
	ProgressRevision    int64
	WatchedAt           *time.Time
	CompletedAt         *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// WatchStartedSeconds and WatchStartedFraction define the smaller playback
// threshold that marks a recording as watched, independently of resume preferences.
const (
	WatchStartedSeconds  = 30.0
	WatchStartedFraction = 0.1
)

// VideoPageCursor is the stable keyset cursor for channel/category video lists.
// start_download_at is the primary sort; id breaks same-timestamp ties.
type VideoPageCursor struct {
	StartDownloadAt time.Time
	ID              int64
}

// VideoPage is one cursor-paginated slice plus the next keyset cursor.
// NextCursor is nil when there are no more rows.
type VideoPage struct {
	Items      []Video
	NextCursor *VideoPageCursor
}

// VideoListPageCursor preserves the active sort value for video.listPage.
// SortInt holds bytes for size or Unix milliseconds for last_watched;
// SortNumber holds duration, SortText holds channel, and SortTime holds derived
// timestamps. StartDownloadAt and ID identify the last returned recording.
type VideoListPageCursor struct {
	SortNumber      *float64
	SortInt         *int64
	SortText        *string
	SortTime        *time.Time
	StartDownloadAt time.Time
	ID              int64
}

// VideoListPage is one cursor-paginated slice of the main videos list.
type VideoListPage struct {
	Items      []Video
	NextCursor *VideoListPageCursor
}

// SortKey returns the normalized sort and order joined for SQL queries.
func (o ListVideosOpts) SortKey() string {
	sort, order := NormalizeVideoListSort(o)
	return sort + "-" + order
}

// WebhookMessageType enumerates the three Twitch EventSub message types.
// Stored on webhook_events.message_type with a CHECK constraint matching
// these values.
const (
	WebhookMessageNotification = "notification"
	WebhookMessageVerification = "webhook_callback_verification"
	WebhookMessageRevocation   = "revocation"
)

// WebhookEventStatus enumerates the handler lifecycle states.
const (
	WebhookStatusReceived  = "received"
	WebhookStatusProcessed = "processed"
	WebhookStatusFailed    = "failed"
)

// DownloadSchedule is a user-defined auto-record rule matched against
// incoming stream.online webhooks.
type DownloadSchedule struct {
	ID            int64
	BroadcasterID string
	RequestedBy   string
	// RequestedFrom identifies the requester; RequestedBy is the approving
	// admin. RequestedFrom is nil for directly created schedules.
	RequestedFrom    *string
	RecordingType    string
	Quality          string
	ForceH264        bool
	HasMinViewers    bool
	MinViewers       *int64
	HasCategories    bool
	HasTags          bool
	IsDeleteRediff   bool
	TimeBeforeDelete *int64
	IsDisabled       bool
	LastTriggeredAt  *time.Time
	TriggerCount     int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// ScheduleInput captures the fields a caller supplies on create/update.
// ID/timestamps/trigger counters are server-managed.
type ScheduleInput struct {
	BroadcasterID    string
	RequestedBy      string
	RequestedFrom    *string
	RecordingType    string
	Quality          string
	ForceH264        bool
	HasMinViewers    bool
	MinViewers       *int64
	HasCategories    bool
	HasTags          bool
	IsDeleteRediff   bool
	TimeBeforeDelete *int64
	IsDisabled       bool
}

// ScheduleFilterInput carries the categories and tags written atomically with a
// schedule.
type ScheduleFilterInput struct {
	CategoryIDs []string
	TagIDs      []int64
}

// Subscription is our local mirror of a Twitch EventSub subscription.
// Condition carries the type-specific JSON (e.g., {"broadcaster_user_id":...}).
type Subscription struct {
	ID                string
	Status            string
	Type              string
	Version           string
	Cost              int64
	Condition         json.RawMessage
	BroadcasterID     *string
	TransportMethod   string
	TransportCallback string
	TwitchCreatedAt   time.Time
	CreatedAt         time.Time
	RevokedAt         *time.Time
	RevokedReason     *string
}

// SubscriptionInput mirrors Twitch's subscription creation response.
type SubscriptionInput struct {
	ID                string
	Status            string
	Type              string
	Version           string
	Cost              int64
	Condition         json.RawMessage
	BroadcasterID     *string
	TransportMethod   string
	TransportCallback string
	TwitchCreatedAt   time.Time
}

type EventSubSnapshot struct {
	ID           int64
	Total        int64
	TotalCost    int64
	MaxTotalCost int64
	FetchedAt    time.Time
}

// SnapshotSubscription pins a subscription's state at snapshot time so
// historical queries don't silently return current values.
type SnapshotSubscription struct {
	SnapshotID       int64
	SubscriptionID   string
	CostAtSnapshot   int64
	StatusAtSnapshot string
}

// WebhookEvent stores a received EventSub event; retention clears its raw Payload
// after webhook_event_payload_retention_days.
type WebhookEvent struct {
	ID               int64
	EventID          string
	MessageType      string
	EventType        *string
	SubscriptionID   *string
	BroadcasterID    *string
	MessageTimestamp time.Time
	Payload          json.RawMessage
	Status           string
	Error            *string
	ReceivedAt       time.Time
	ProcessedAt      *time.Time
}

// WebhookEventInput records an incoming webhook before processing begins.
type WebhookEventInput struct {
	EventID          string
	MessageType      string
	EventType        *string
	SubscriptionID   *string
	BroadcasterID    *string
	MessageTimestamp time.Time
	Payload          json.RawMessage
}

const (
	RecordingWebhookDeliveryPending    = "pending"
	RecordingWebhookDeliveryDelivering = "delivering"
	RecordingWebhookDeliveryDelivered  = "delivered"
	RecordingWebhookDeliveryRejected   = "rejected"
	RecordingWebhookDeliveryFailed     = "failed"
)

// RecordingWebhookDelivery stores an outbound recording event until settlement;
// a crash after sending can resend the same MessageID.
type RecordingWebhookDelivery struct {
	ID            int64
	MessageID     string
	DedupeKey     string
	Event         string
	VideoID       int64
	Status        string
	Attempts      int
	LastStatus    int
	LastError     string
	Test          bool
	NextAttemptAt time.Time
	LastAttemptAt *time.Time
	DeliveredAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	// FrozenParts holds serialized paths, sizes, and indices after the first
	// delivery build so retries survive part deletion; signed URLs are minted
	// per attempt and excluded from this snapshot.
	FrozenParts string
}

// RecordingWebhookDeliveryInput creates a pending delivery. DedupeKey is unique
// so retrying the terminal transition for the same recording outcome returns the
// original row instead of creating a duplicate.
type RecordingWebhookDeliveryInput struct {
	MessageID     string
	DedupeKey     string
	Event         string
	VideoID       int64
	Test          bool
	NextAttemptAt time.Time
}

// TaskStatus enumerates the lifecycle values for scheduled tasks.
// Stored on tasks.last_status with a CHECK constraint matching these.
const (
	TaskStatusPending     = "pending"
	TaskStatusRunning     = "running"
	TaskStatusSuccess     = "success"
	TaskStatusFailed      = "failed"
	TaskStatusSkipped     = "skipped"
	TaskStatusInterrupted = "interrupted"
)

// Task stores operator enablement and run history across process restarts;
// IsAvailable reflects the current process's registered capabilities.
type Task struct {
	ExecutionID     string
	Name            string
	Description     string
	IntervalSeconds int32
	IsEnabled       bool
	IsAvailable     bool
	LastRunAt       *time.Time
	LastDurationMs  int32
	LastStatus      string
	LastError       *string
	NextRunAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// EventLogSeverity enumerates severities stored on event_logs.severity.
const (
	EventLogSeverityDebug = "debug"
	EventLogSeverityInfo  = "info"
	EventLogSeverityWarn  = "warn"
	EventLogSeverityError = "error"
)

// EventLog records an application action in the append-only audit log.
type EventLog struct {
	ID          int64
	Domain      string
	EventType   string
	Severity    string
	Message     string
	ActorUserID *string
	Data        json.RawMessage
	CreatedAt   time.Time
}

// EventLogInput is the create payload for CreateEventLog. Data is
// optional JSON context; empty means no structured data.
type EventLogInput struct {
	Domain      string
	EventType   string
	Severity    string
	Message     string
	ActorUserID *string
	Data        json.RawMessage
}

type Settings struct {
	ResumeMinSeconds       int64
	ResumeEndMarginSeconds int64
	ResumeEndMarginPercent int64
	UserID                 string
	Timezone               string
	DatetimeFormat         string
	Language               string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// ServerSettings stores process-wide settings that are configured through the
// owner UI rather than environment variables.
type ServerSettings struct {
	ServerMode                    string
	EventSubWebhookCallbackURL    string
	EventSubRelayIngestURL        string
	EventSubRelaySubscribeURL     string
	EventSubRelayLocalCallbackURL string
	// RecordingWebhookEnabled gates dispatch; an empty secret needs initialization,
	// and empty RecordingWebhookEvents selects all terminal recording events.
	RecordingWebhookEnabled   bool
	RecordingWebhookURL       string
	RecordingWebhookSecret    string
	RecordingWebhookEvents    string
	PlaybackCacheEnabled      bool
	PlaybackCacheMaxPercent   int
	PlaybackCacheAutoGenerate bool
	// SchedulesPaused is the global auto-download kill switch. When true, the
	// schedule processor skips every stream.online auto-download without
	// touching any schedule's IsDisabled, so resuming restores prior state.
	SchedulesPaused bool
	// StorageID is the identity the attached storage must carry in its marker;
	// empty until the first attach. StorageScanCursor is the storage scan's
	// persisted resume position.
	StorageID         string
	StorageScanCursor int64
	// StorageRestoreCursor is nil between passes, zero before the first page,
	// or the last completed restore page's id while a pass is pending.
	StorageRestoreCursor *int64
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// PlaybackAssetCursor preserves the complete LRU order across bounded pages.
type PlaybackAssetCursor struct {
	AccessedAt  time.Time
	GeneratedAt time.Time
	VideoID     int64
}

// PlaybackCursor returns the position after v in playback cache eviction order.
func PlaybackCursor(v VideoPlaybackAsset) PlaybackAssetCursor {
	c := PlaybackAssetCursor{VideoID: v.VideoID}
	if v.LastAccessedAt != nil {
		c.AccessedAt = *v.LastAccessedAt
	}
	if v.GeneratedAt != nil {
		c.GeneratedAt = *v.GeneratedAt
	}
	return c
}

// ArchiveQueueCandidate orders a pending archive by its original request time.
type ArchiveQueueCandidate struct {
	JobID    string
	VideoID  int64
	QueuedAt time.Time
}
