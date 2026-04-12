package repository

import "time"

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
