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
