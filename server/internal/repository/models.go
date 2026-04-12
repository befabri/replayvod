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
type Video struct {
	ID              int64
	JobID           string
	Filename        string
	DisplayName     string
	Status          string
	Quality         string
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
}

// VideoInput is the creation payload for a new download row.
type VideoInput struct {
	JobID         string
	Filename      string
	DisplayName   string
	Status        string
	Quality       string
	BroadcasterID string
	StreamID      *string
	ViewerCount   int64
	Language      string
}

// Title is a stream/video title string (deduplicated by name).
type Title struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

// VideoStatsTotals is the aggregate row for video.statistics.
type VideoStatsTotals struct {
	Total         int64
	TotalSize     int64
	TotalDuration float64
}

// VideoStatsByStatus is one bucket of the status histogram.
type VideoStatsByStatus struct {
	Status string
	Count  int64
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
	ID                int64
	BroadcasterID     string
	RequestedBy       string
	Quality           string
	HasMinViewers     bool
	MinViewers        *int64
	HasCategories     bool
	HasTags           bool
	IsDeleteRediff    bool
	TimeBeforeDelete  *int64
	IsDisabled        bool
	LastTriggeredAt   *time.Time
	TriggerCount      int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
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
	ID            int64
	Total         int64
	TotalCost     int64
	MaxTotalCost  int64
	FetchedAt     time.Time
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
