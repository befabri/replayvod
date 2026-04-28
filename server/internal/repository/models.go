package repository

import (
	"encoding/json"
	"time"
)

// User is the domain model for an authenticated Twitch user.
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

// Session is a server-side session. The cookie holds only the raw session ID;
// the hashed_id (SHA-256 of the raw ID) is stored and looked up in the DB.
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

// SessionInfo is a read-only view of a session (no encrypted tokens).
type SessionInfo struct {
	HashedID     string
	UserID       string
	ExpiresAt    time.Time
	LastActiveAt time.Time
	UserAgent    *string
	IPAddress    *string
	CreatedAt    time.Time
}

// AppAccessToken is a Twitch app access token (client credentials).
type AppAccessToken struct {
	ID        int64
	Token     string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// WhitelistEntry is an allowed Twitch user ID.
type WhitelistEntry struct {
	TwitchUserID string
	AddedAt      time.Time
}

// Channel is a Twitch broadcaster channel.
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

// ChannelPageCursor is the stable keyset cursor for channel lists.
// broadcaster_name is compared case-insensitively; broadcaster_id breaks ties.
type ChannelPageCursor struct {
	BroadcasterName string
	BroadcasterID   string
}

// ChannelPage is one cursor-paginated slice of channels.
type ChannelPage struct {
	Items      []Channel
	NextCursor *ChannelPageCursor
}

// UserFollow tracks a user's follow relationship with a channel.
type UserFollow struct {
	UserID        string
	BroadcasterID string
	FollowedAt    time.Time
	Followed      bool
}

// Category is a Twitch game/category.
type Category struct {
	ID        string
	Name      string
	BoxArtURL *string
	IGDBID    *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Tag is a stream tag.
type Tag struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

// FetchLog is an audit entry for a Twitch API call.
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

// FetchLogInput is the input for CreateFetchLog.
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
)

// Stream is a single Twitch broadcast session.
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

// LatestLiveStream is the most recent stream for a broadcaster, flattened
// with the channel display metadata (login, name, avatar). Returned by
// ListLatestLivePerChannel so the dashboard can render a "recently live"
// feed without N+1 channel lookups. Ordered newest-first by StartedAt.
type LatestLiveStream struct {
	Stream
	BroadcasterLogin string
	BroadcasterName  string
	ProfileImageURL  *string
}

// StreamInput is the upsert payload for a stream snapshot.
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

// Video is a downloaded VOD or an in-flight download. Status drives UI state
// (PENDING queued → RUNNING downloading → DONE/FAILED terminal).
//
// ForceH264 is a per-job codec preference: when true the downloader's
// Stage 3 variant picker drops HEVC and AV1 variants before running
// the quality fallback chain. Ignored when RecordingType='audio'.
type Video struct {
	ID          int64
	JobID       string
	Filename    string
	DisplayName string
	// Title is the stream title at download-start time — the thing
	// the streamer typed as the broadcast label (e.g. "Playing ER
	// DLC"), not the channel's display name. Empty string when
	// Twitch didn't surface a title (rare; manual trigger against
	// a channel that just went offline).
	Title  string
	Status string
	// Quality is the requested operator intent at trigger time
	// (LOW/MEDIUM/HIGH). The selected rendition that Stage 3 actually
	// recorded lives on SelectedQuality/SelectedFPS.
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
	RecordingType   string
	ForceH264       bool
	// CompletionKind distinguishes content-completeness from
	// pipeline success. Values: "complete" (clean end), "partial"
	// (ended but missed data — typically a shutdown-resume gap),
	// "cancelled" (operator called Cancel). Defaults to "complete"
	// on insert; final value is set at terminal transitions in the
	// downloader.
	CompletionKind string
	// Truncated is the orthogonal stop-boundary axis: true when the
	// recording stopped before the broadcast did (operator cancel,
	// failed mid-run, or finalized without ever observing
	// EXT-X-ENDLIST). False for clean DONE recordings that captured
	// up to the playlist's natural end. Independent of
	// CompletionKind: a recording can be both partial AND truncated
	// (CDN rolled past us, then we stopped while live), or complete
	// AND truncated (file is whole but the broadcast went on).
	Truncated bool
}

// VideoCompletionKind enumerates the values of videos.completion_kind.
// Pinned to constants so the downloader and handler don't drift on
// literals.
const (
	CompletionKindComplete  = "complete"
	CompletionKindPartial   = "partial"
	CompletionKindCancelled = "cancelled"
)

// VideoInput is the creation payload for a new download row.
// RecordingType is required ("video" or "audio"); empty defaults to "video"
// at the adapter layer. ForceH264 defaults to false.
type VideoInput struct {
	JobID       string
	Filename    string
	DisplayName string
	// Title is the stream title at download-start time; pass ""
	// when Helix didn't surface one so the row still satisfies the
	// NOT NULL constraint.
	Title         string
	Status        string
	Quality       string
	BroadcasterID string
	StreamID      *string
	ViewerCount   int64
	Language      string
	RecordingType string
	ForceH264     bool
}

// RecordingType enumerates the two recording modes. Stored on
// videos.recording_type with a CHECK constraint matching these values.
const (
	RecordingTypeVideo = "video"
	RecordingTypeAudio = "audio"
)

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

// Job is the durable record of a download execution. One row per attempt
// at turning a live stream into a stored VOD; the `videos` row is the
// logical output, jobs accumulate over retries. ResumeState is a JSON blob
// whose schema is documented in .docs/spec/download-pipeline.md under
// "Resume on restart".
type Job struct {
	ID            string
	VideoID       int64
	BroadcasterID string
	Status        string
	StartedAt     *time.Time
	FinishedAt    *time.Time
	Error         *string
	ResumeState   json.RawMessage
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// JobInput is the creation payload for a new job row. Status defaults to
// PENDING at the SQL layer. ResumeState empty means "no checkpoint yet"
// — the adapter sends `{}` so the NOT NULL column stays unmarshal-safe.
type JobInput struct {
	ID            string
	VideoID       int64
	BroadcasterID string
	ResumeState   json.RawMessage
}

// VideoPart is one output segment of a video. A job with no
// variant/codec/container switch produces exactly one part. A job that
// splits on variant loss, codec change, container change, or restart-gap
// threshold produces 2..N parts, ordered by part_index.
//
// EndMediaSeq is nullable: the value is only known at FinalizeVideoPart.
// A NULL EndMediaSeq means "part created, not yet finalized" — distinct
// from a zero-length finalized part (which would have
// EndMediaSeq == &StartMediaSeq).
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

// VideoPartInput is the creation payload for a new part row. Duration,
// size, thumbnail, and end_media_seq are filled in at finalization via
// FinalizeVideoPart — they aren't known until Stage 5+ of the pipeline.
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

// VideoPartFinalize is the update payload for FinalizeVideoPart — the
// fields that are only known after Stage 5-8 of the pipeline complete.
// Callers must always supply EndMediaSeq here; leaving it unset on a
// finalize call is a caller bug.
type VideoPartFinalize struct {
	ID              int64
	DurationSeconds float64
	SizeBytes       int64
	Thumbnail       *string
	EndMediaSeq     int64
}

// Title is a stream/video title string (deduplicated by name).
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

// CategorySpan is one concrete interval during which a video carried a category.
type CategorySpan struct {
	Category
	StartedAt       time.Time
	EndedAt         *time.Time
	DurationSeconds float64
}

// VideoMetadataChange is one observed channel.update event tied to a
// recording: at OccurredAt, the stream had Title and/or Category set
// to these values. Each event captures every dimension observed in
// that update — usually both, sometimes only one.
//
// Title and Category are nil when that dimension wasn't part of the
// triggering event (not "the stream had no title", just "this event
// didn't carry one"). The CHECK constraint on the table forbids
// rows where both are nil.
type VideoMetadataChange struct {
	ID         int64
	VideoID    int64
	OccurredAt time.Time
	Title      *Title
	Category   *Category
}

// VideoMetadataChangeInput carries one channel.update observation
// (or LinkInitialVideoMetadata download-trigger snapshot). Title and
// CategoryID empty mean "not observed in this event"; the adapter
// short-circuits and returns ErrNoMetadataObserved without writing.
// CategoryName is the optional companion to CategoryID — empty means
// "don't refresh categories.name" so an existing good name isn't
// clobbered by a partial Helix payload.
type VideoMetadataChangeInput struct {
	VideoID      int64
	OccurredAt   time.Time
	Title        string
	CategoryID   string
	CategoryName string
}

// VideoMetadataChangeResult exposes the upserted title row and the
// category row (when written) so the caller can drive post-tx side
// effects — currently just the box-art enrich on first observation
// of a category — without re-querying.
type VideoMetadataChangeResult struct {
	Title    *Title
	Category *Category
}

// VideoStatsTotals is the aggregate row for video.statistics.
// Total/TotalSize/TotalDuration are DONE-only rollups (the user-
// visible "N recordings · X GB" subtitle). ThisWeek and Incomplete
// run across all non-deleted rows and feed the videos page tab
// counters. Incomplete = completion_kind='partial' OR truncated,
// matching the Partial tab's broader server-side filter.
type VideoStatsTotals struct {
	Total         int64
	TotalSize     int64
	TotalDuration float64
	ThisWeek      int64
	Incomplete    int64
	Channels      int64
}

// VideoStatsByStatus is one bucket of the status histogram.
type VideoStatsByStatus struct {
	Status string
	Count  int64
}

// ListVideosOpts is the filter+sort payload for ListVideos. Empty Status
// means "all statuses". Sort/Order are enum-validated at the handler
// boundary; the adapter falls back to created_at DESC when they are
// empty or unrecognized. NULL size/duration rows sort to the end.
type ListVideosOpts struct {
	Status             string // "" | "PENDING" | "RUNNING" | "DONE" | "FAILED"
	Sort               string // "" | "created_at" | "duration" | "size" | "channel"
	Order              string // "" | "asc" | "desc"
	Quality            string
	BroadcasterID      string
	Language           string
	DurationMinSeconds *float64
	DurationMaxSeconds *float64
	SizeMinBytes       *int64
	SizeMaxBytes       *int64
	// Window is a coarse recency filter resolved to a server-side
	// `start_download_at >= now() - <interval>` predicate. "" means
	// "no recency filter". The dashboard tab system uses "this_week".
	Window string // "" | "this_week"
	// IncompleteOnly narrows the result to "anything that didn't
	// capture the full broadcast" — completion_kind='partial' OR
	// truncated. Powers the dashboard's Partial tab, which buckets
	// gap-rooted partial files alongside cancelled and truncated
	// recordings under one user-facing concept.
	IncompleteOnly bool
	Limit          int
	Offset         int
}

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

// VideoListPageCursor is the stable keyset cursor for video.listPage.
// SortNumber is used for duration sorts, SortInt for size sorts, SortText for channel sorts,
// and StartDownloadAt always participates as a stable tie-breaker.
type VideoListPageCursor struct {
	SortNumber      *float64
	SortInt         *int64
	SortText        *string
	StartDownloadAt time.Time
	ID              int64
}

// VideoListPage is one cursor-paginated slice of the main videos list.
type VideoListPage struct {
	Items      []Video
	NextCursor *VideoListPageCursor
}

// SortKey composes Sort+Order into the "sort_key" form the SQL expects
// ("duration-desc", "channel-asc", etc.). Returns "" when either half
// is empty — the SQL's default branch (start_download_at DESC) handles
// that case without needing a second code path. Handlers that want
// "desc when sort is set, default otherwise" should fill Order before
// calling.
func (o ListVideosOpts) SortKey() string {
	if o.Sort == "" || o.Order == "" {
		return ""
	}
	return o.Sort + "-" + o.Order
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
	ID               int64
	BroadcasterID    string
	RequestedBy      string
	Quality          string
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
	Quality          string
	HasMinViewers    bool
	MinViewers       *int64
	HasCategories    bool
	HasTags          bool
	IsDeleteRediff   bool
	TimeBeforeDelete *int64
	IsDisabled       bool
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

// SubscriptionInput is the create payload, matching what Twitch returns
// when we call CreateEventSubSubscription.
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

// EventSubSnapshot is a periodic snapshot of aggregate subscription cost.
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

// WebhookEvent is one received EventSub webhook in the audit log.
// Payload is the raw Twitch body; nulled out by the retention task after
// webhook_event_payload_retention_days. See migration comments for the
// message_type / status state machine.
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

// WebhookEventInput is the create payload for audit logging a received
// webhook. The handler fills this before dispatching to the event-type
// specific processor.
type WebhookEventInput struct {
	EventID          string
	MessageType      string
	EventType        *string
	SubscriptionID   *string
	BroadcasterID    *string
	MessageTimestamp time.Time
	Payload          json.RawMessage
}

// TaskStatus enumerates the lifecycle values for scheduled tasks.
// Stored on tasks.last_status with a CHECK constraint matching these.
const (
	TaskStatusPending = "pending"
	TaskStatusRunning = "running"
	TaskStatusSuccess = "success"
	TaskStatusFailed  = "failed"
	TaskStatusSkipped = "skipped"
)

// Task is a registered scheduled background job. Runtime state
// (last_run_at, last_status, next_run_at) is mutated by the scheduler
// on each invocation; descriptive columns (name, description,
// interval_seconds) are registered on startup and respected across
// restarts. See queries/*/tasks.sql for the state-transition SQL.
type Task struct {
	Name            string
	Description     string
	IntervalSeconds int32
	IsEnabled       bool
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

// EventLog is an append-only app-side audit entry. Distinct from
// webhook_events (inbound Twitch) and fetch_logs (outbound Helix):
// event_logs records what the app itself did.
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

// Settings is a user's display preferences. One row per user, keyed
// by users.id with ON DELETE CASCADE.
type Settings struct {
	UserID         string
	Timezone       string
	DatetimeFormat string
	Language       string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
