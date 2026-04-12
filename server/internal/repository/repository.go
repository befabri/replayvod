package repository

import (
	"context"
	"time"
)

// Repository is the common interface for database access.
// Both PG and SQLite adapters implement this.
type Repository interface {
	// Users
	GetUser(ctx context.Context, id string) (*User, error)
	GetUserByLogin(ctx context.Context, login string) (*User, error)
	UpsertUser(ctx context.Context, u *User) (*User, error)
	ListUsers(ctx context.Context) ([]User, error)
	UpdateUserRole(ctx context.Context, id string, role string) error

	// Sessions
	CreateSession(ctx context.Context, s *Session) error
	GetSession(ctx context.Context, hashedID string) (*Session, error)
	UpdateSessionTokens(ctx context.Context, hashedID string, encryptedTokens []byte) error
	UpdateSessionActivity(ctx context.Context, hashedID string) error
	DeleteSession(ctx context.Context, hashedID string) error
	DeleteUserSessions(ctx context.Context, userID string) error
	DeleteExpiredSessions(ctx context.Context) error
	ListUserSessions(ctx context.Context, userID string) ([]SessionInfo, error)

	// App Access Tokens
	GetLatestAppToken(ctx context.Context) (*AppAccessToken, error)
	CreateAppToken(ctx context.Context, token string, expiresAt time.Time) (*AppAccessToken, error)
	DeleteExpiredAppTokens(ctx context.Context) error

	// Whitelist
	IsWhitelisted(ctx context.Context, twitchUserID string) (bool, error)
	AddToWhitelist(ctx context.Context, twitchUserID string) error
	RemoveFromWhitelist(ctx context.Context, twitchUserID string) error
	ListWhitelist(ctx context.Context) ([]WhitelistEntry, error)
}
