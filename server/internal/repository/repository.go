package repository

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when a requested entity does not exist.
// Both PG and SQLite adapters translate their driver-specific "no rows"
// errors to this sentinel so services can branch on it portably.
var ErrNotFound = errors.New("repository: not found")

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

	// Channels
	GetChannel(ctx context.Context, broadcasterID string) (*Channel, error)
	GetChannelByLogin(ctx context.Context, login string) (*Channel, error)
	UpsertChannel(ctx context.Context, c *Channel) (*Channel, error)
	ListChannels(ctx context.Context) ([]Channel, error)
	DeleteChannel(ctx context.Context, broadcasterID string) error

	// User follows
	UpsertUserFollow(ctx context.Context, f *UserFollow) error
	ListUserFollows(ctx context.Context, userID string) ([]Channel, error)
	UnfollowChannel(ctx context.Context, userID, broadcasterID string) error

	// Categories
	GetCategory(ctx context.Context, id string) (*Category, error)
	GetCategoryByName(ctx context.Context, name string) (*Category, error)
	UpsertCategory(ctx context.Context, c *Category) (*Category, error)
	ListCategories(ctx context.Context) ([]Category, error)
	ListCategoriesMissingBoxArt(ctx context.Context) ([]Category, error)

	// Tags
	GetTag(ctx context.Context, id int64) (*Tag, error)
	GetTagByName(ctx context.Context, name string) (*Tag, error)
	UpsertTag(ctx context.Context, name string) (*Tag, error)
	ListTags(ctx context.Context) ([]Tag, error)

	// Fetch logs
	CreateFetchLog(ctx context.Context, input *FetchLogInput) error
	ListFetchLogs(ctx context.Context, limit, offset int) ([]FetchLog, error)
	ListFetchLogsByType(ctx context.Context, fetchType string, limit, offset int) ([]FetchLog, error)
	CountFetchLogs(ctx context.Context) (int64, error)
	CountFetchLogsByType(ctx context.Context, fetchType string) (int64, error)
	DeleteOldFetchLogs(ctx context.Context, before time.Time) error
}
