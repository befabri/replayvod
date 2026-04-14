package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

// mapErr translates database/sql driver errors to portable repository errors.
// Callers that need a not-found branch should `errors.Is(err, repository.ErrNotFound)`.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return repository.ErrNotFound
	}
	return err
}

// SQLiteAdapter implements repository.Repository using SQLite via sqlc-generated code.
//
// db is kept alongside queries because a few queries (dynamic ORDER BY
// on ListVideos, ranked SearchChannels) use CASE expressions that sqlc's
// SQLite engine can't type-infer — those are run as raw SQL through db
// directly, while everything else goes through queries.
type SQLiteAdapter struct {
	queries *sqlitegen.Queries
	db      sqlitegen.DBTX
}

// New creates a new SQLiteAdapter. db is typically an *sql.DB but any
// sqlitegen.DBTX works — the adapter retains it so the hand-rolled
// queries can reach the raw driver without fighting sqlc.
func New(db sqlitegen.DBTX) *SQLiteAdapter {
	return &SQLiteAdapter{queries: sqlitegen.New(db), db: db}
}

func (a *SQLiteAdapter) Ping(ctx context.Context) error {
	var n int
	if err := a.db.QueryRowContext(ctx, "SELECT 1").Scan(&n); err != nil {
		return fmt.Errorf("sqlite ping: %w", err)
	}
	return nil
}

func (a *SQLiteAdapter) GetUser(ctx context.Context, id string) (*repository.User, error) {
	row, err := a.queries.GetUser(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteUserToDomain(row), nil
}

func (a *SQLiteAdapter) GetUserByLogin(ctx context.Context, login string) (*repository.User, error) {
	row, err := a.queries.GetUserByLogin(ctx, login)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteUserToDomain(row), nil
}

func (a *SQLiteAdapter) UpsertUser(ctx context.Context, u *repository.User) (*repository.User, error) {
	row, err := a.queries.UpsertUser(ctx, sqlitegen.UpsertUserParams{
		ID:              u.ID,
		Login:           u.Login,
		DisplayName:     u.DisplayName,
		Email:           toNullString(u.Email),
		ProfileImageUrl: toNullString(u.ProfileImageURL),
		Role:            u.Role,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert user %s: %w", u.ID, err)
	}
	return sqliteUserToDomain(row), nil
}

func (a *SQLiteAdapter) ListUsers(ctx context.Context) ([]repository.User, error) {
	rows, err := a.queries.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite list users: %w", err)
	}
	users := make([]repository.User, len(rows))
	for i, row := range rows {
		users[i] = *sqliteUserToDomain(row)
	}
	return users, nil
}

func (a *SQLiteAdapter) UpdateUserRole(ctx context.Context, id string, role string) error {
	if err := a.queries.UpdateUserRole(ctx, sqlitegen.UpdateUserRoleParams{
		ID:   id,
		Role: role,
	}); err != nil {
		return fmt.Errorf("sqlite update user role %s: %w", id, err)
	}
	return nil
}

// Conversion helpers

func sqliteUserToDomain(u sqlitegen.User) *repository.User {
	return &repository.User{
		ID:              u.ID,
		Login:           u.Login,
		DisplayName:     u.DisplayName,
		Email:           fromNullString(u.Email),
		ProfileImageURL: fromNullString(u.ProfileImageUrl),
		Role:            u.Role,
		CreatedAt:       parseTime(u.CreatedAt),
		UpdatedAt:       parseTime(u.UpdatedAt),
	}
}

func fromNullString(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}

func toNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func parseTime(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

func formatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

// Sessions

func (a *SQLiteAdapter) CreateSession(ctx context.Context, s *repository.Session) error {
	if err := a.queries.CreateSession(ctx, sqlitegen.CreateSessionParams{
		HashedID:        s.HashedID,
		UserID:          s.UserID,
		EncryptedTokens: s.EncryptedTokens,
		ExpiresAt:       formatTime(s.ExpiresAt),
		UserAgent:       toNullString(s.UserAgent),
		IpAddress:       toNullString(s.IPAddress),
	}); err != nil {
		return fmt.Errorf("sqlite create session: %w", err)
	}
	return nil
}

func (a *SQLiteAdapter) GetSession(ctx context.Context, hashedID string) (*repository.Session, error) {
	row, err := a.queries.GetSession(ctx, hashedID)
	if err != nil {
		return nil, fmt.Errorf("sqlite get session: %w", err)
	}
	return &repository.Session{
		HashedID:        row.HashedID,
		UserID:          row.UserID,
		EncryptedTokens: row.EncryptedTokens,
		ExpiresAt:       parseTime(row.ExpiresAt),
		LastActiveAt:    parseTime(row.LastActiveAt),
		UserAgent:       fromNullString(row.UserAgent),
		IPAddress:       fromNullString(row.IpAddress),
		CreatedAt:       parseTime(row.CreatedAt),
	}, nil
}

func (a *SQLiteAdapter) UpdateSessionTokens(ctx context.Context, hashedID string, encryptedTokens []byte) error {
	return a.queries.UpdateSessionTokens(ctx, sqlitegen.UpdateSessionTokensParams{
		HashedID:        hashedID,
		EncryptedTokens: encryptedTokens,
	})
}

func (a *SQLiteAdapter) UpdateSessionActivity(ctx context.Context, hashedID string) error {
	return a.queries.UpdateSessionActivity(ctx, hashedID)
}

func (a *SQLiteAdapter) DeleteSession(ctx context.Context, hashedID string) error {
	return a.queries.DeleteSession(ctx, hashedID)
}

func (a *SQLiteAdapter) DeleteUserSessions(ctx context.Context, userID string) error {
	return a.queries.DeleteUserSessions(ctx, userID)
}

func (a *SQLiteAdapter) DeleteExpiredSessions(ctx context.Context) error {
	return a.queries.DeleteExpiredSessions(ctx)
}

func (a *SQLiteAdapter) ListUserSessions(ctx context.Context, userID string) ([]repository.SessionInfo, error) {
	rows, err := a.queries.ListUserSessions(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("sqlite list user sessions: %w", err)
	}
	sessions := make([]repository.SessionInfo, len(rows))
	for i, row := range rows {
		sessions[i] = repository.SessionInfo{
			HashedID:     row.HashedID,
			UserID:       row.UserID,
			ExpiresAt:    parseTime(row.ExpiresAt),
			LastActiveAt: parseTime(row.LastActiveAt),
			UserAgent:    fromNullString(row.UserAgent),
			IPAddress:    fromNullString(row.IpAddress),
			CreatedAt:    parseTime(row.CreatedAt),
		}
	}
	return sessions, nil
}

// App Access Tokens

func (a *SQLiteAdapter) GetLatestAppToken(ctx context.Context) (*repository.AppAccessToken, error) {
	row, err := a.queries.GetLatestAppToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite get latest app token: %w", err)
	}
	return &repository.AppAccessToken{
		ID:        row.ID,
		Token:     row.Token,
		ExpiresAt: parseTime(row.ExpiresAt),
		CreatedAt: parseTime(row.CreatedAt),
	}, nil
}

func (a *SQLiteAdapter) CreateAppToken(ctx context.Context, token string, expiresAt time.Time) (*repository.AppAccessToken, error) {
	row, err := a.queries.CreateAppToken(ctx, sqlitegen.CreateAppTokenParams{
		Token:     token,
		ExpiresAt: formatTime(expiresAt),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite create app token: %w", err)
	}
	return &repository.AppAccessToken{
		ID:        row.ID,
		Token:     row.Token,
		ExpiresAt: parseTime(row.ExpiresAt),
		CreatedAt: parseTime(row.CreatedAt),
	}, nil
}

func (a *SQLiteAdapter) DeleteExpiredAppTokens(ctx context.Context) error {
	return a.queries.DeleteExpiredAppTokens(ctx)
}

// Whitelist

func (a *SQLiteAdapter) IsWhitelisted(ctx context.Context, twitchUserID string) (bool, error) {
	return a.queries.IsWhitelisted(ctx, twitchUserID)
}

func (a *SQLiteAdapter) AddToWhitelist(ctx context.Context, twitchUserID string) error {
	return a.queries.AddToWhitelist(ctx, twitchUserID)
}

func (a *SQLiteAdapter) RemoveFromWhitelist(ctx context.Context, twitchUserID string) error {
	return a.queries.RemoveFromWhitelist(ctx, twitchUserID)
}

func (a *SQLiteAdapter) ListWhitelist(ctx context.Context) ([]repository.WhitelistEntry, error) {
	rows, err := a.queries.ListWhitelist(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite list whitelist: %w", err)
	}
	entries := make([]repository.WhitelistEntry, len(rows))
	for i, row := range rows {
		entries[i] = repository.WhitelistEntry{
			TwitchUserID: row.TwitchUserID,
			AddedAt:      parseTime(row.AddedAt),
		}
	}
	return entries, nil
}
