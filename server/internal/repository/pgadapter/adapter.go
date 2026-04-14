package pgadapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

// mapErr translates pgx driver errors to portable repository errors.
// Callers that need a not-found branch should `errors.Is(err, repository.ErrNotFound)`.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.ErrNotFound
	}
	return err
}

// PGAdapter implements repository.Repository using PostgreSQL via sqlc-generated code.
// sqlc.yaml maps nullable columns to *T so this adapter doesn't need pgtype shuffling.
//
// db is kept alongside queries so PG-only capabilities (FullTextSearcher,
// future LISTEN/NOTIFY) can reach the raw connection for queries that
// live outside the sqlc-generated surface.
type PGAdapter struct {
	queries *pggen.Queries
	db      pggen.DBTX
}

// New creates a new PGAdapter. db is the pgx pool or transaction
// backing the generated queries; it's retained so the adapter can run
// raw SQL for PG-only capabilities without fighting sqlc.
func New(db pggen.DBTX) *PGAdapter {
	return &PGAdapter{queries: pggen.New(db), db: db}
}

func (a *PGAdapter) Ping(ctx context.Context) error {
	var n int
	if err := a.db.QueryRow(ctx, "SELECT 1").Scan(&n); err != nil {
		return fmt.Errorf("pg ping: %w", err)
	}
	return nil
}

// Users

func (a *PGAdapter) GetUser(ctx context.Context, id string) (*repository.User, error) {
	row, err := a.queries.GetUser(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgUserToDomain(row), nil
}

func (a *PGAdapter) GetUserByLogin(ctx context.Context, login string) (*repository.User, error) {
	row, err := a.queries.GetUserByLogin(ctx, login)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgUserToDomain(row), nil
}

func (a *PGAdapter) UpsertUser(ctx context.Context, u *repository.User) (*repository.User, error) {
	row, err := a.queries.UpsertUser(ctx, pggen.UpsertUserParams{
		ID:              u.ID,
		Login:           u.Login,
		DisplayName:     u.DisplayName,
		Email:           u.Email,
		ProfileImageUrl: u.ProfileImageURL,
		Role:            u.Role,
	})
	if err != nil {
		return nil, fmt.Errorf("pg upsert user %s: %w", u.ID, err)
	}
	return pgUserToDomain(row), nil
}

func (a *PGAdapter) ListUsers(ctx context.Context) ([]repository.User, error) {
	rows, err := a.queries.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg list users: %w", err)
	}
	users := make([]repository.User, len(rows))
	for i, row := range rows {
		users[i] = *pgUserToDomain(row)
	}
	return users, nil
}

func (a *PGAdapter) UpdateUserRole(ctx context.Context, id string, role string) error {
	if err := a.queries.UpdateUserRole(ctx, pggen.UpdateUserRoleParams{ID: id, Role: role}); err != nil {
		return fmt.Errorf("pg update user role %s: %w", id, err)
	}
	return nil
}

func pgUserToDomain(u pggen.User) *repository.User {
	return &repository.User{
		ID:              u.ID,
		Login:           u.Login,
		DisplayName:     u.DisplayName,
		Email:           u.Email,
		ProfileImageURL: u.ProfileImageUrl,
		Role:            u.Role,
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

// Sessions

func (a *PGAdapter) CreateSession(ctx context.Context, s *repository.Session) error {
	if err := a.queries.CreateSession(ctx, pggen.CreateSessionParams{
		HashedID:        s.HashedID,
		UserID:          s.UserID,
		EncryptedTokens: s.EncryptedTokens,
		ExpiresAt:       s.ExpiresAt,
		UserAgent:       s.UserAgent,
		IpAddress:       s.IPAddress,
	}); err != nil {
		return fmt.Errorf("pg create session: %w", err)
	}
	return nil
}

func (a *PGAdapter) GetSession(ctx context.Context, hashedID string) (*repository.Session, error) {
	row, err := a.queries.GetSession(ctx, hashedID)
	if err != nil {
		return nil, mapErr(err)
	}
	return &repository.Session{
		HashedID:        row.HashedID,
		UserID:          row.UserID,
		EncryptedTokens: row.EncryptedTokens,
		ExpiresAt:       row.ExpiresAt,
		LastActiveAt:    row.LastActiveAt,
		UserAgent:       row.UserAgent,
		IPAddress:       row.IpAddress,
		CreatedAt:       row.CreatedAt,
	}, nil
}

func (a *PGAdapter) UpdateSessionTokens(ctx context.Context, hashedID string, encryptedTokens []byte) error {
	return a.queries.UpdateSessionTokens(ctx, pggen.UpdateSessionTokensParams{
		HashedID:        hashedID,
		EncryptedTokens: encryptedTokens,
	})
}

func (a *PGAdapter) UpdateSessionActivity(ctx context.Context, hashedID string) error {
	return a.queries.UpdateSessionActivity(ctx, hashedID)
}

func (a *PGAdapter) DeleteSession(ctx context.Context, hashedID string) error {
	return a.queries.DeleteSession(ctx, hashedID)
}

func (a *PGAdapter) DeleteUserSessions(ctx context.Context, userID string) error {
	return a.queries.DeleteUserSessions(ctx, userID)
}

func (a *PGAdapter) DeleteExpiredSessions(ctx context.Context) error {
	return a.queries.DeleteExpiredSessions(ctx)
}

func (a *PGAdapter) ListUserSessions(ctx context.Context, userID string) ([]repository.SessionInfo, error) {
	rows, err := a.queries.ListUserSessions(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("pg list user sessions: %w", err)
	}
	sessions := make([]repository.SessionInfo, len(rows))
	for i, row := range rows {
		sessions[i] = repository.SessionInfo{
			HashedID:     row.HashedID,
			UserID:       row.UserID,
			ExpiresAt:    row.ExpiresAt,
			LastActiveAt: row.LastActiveAt,
			UserAgent:    row.UserAgent,
			IPAddress:    row.IpAddress,
			CreatedAt:    row.CreatedAt,
		}
	}
	return sessions, nil
}

// App Access Tokens

func (a *PGAdapter) GetLatestAppToken(ctx context.Context) (*repository.AppAccessToken, error) {
	row, err := a.queries.GetLatestAppToken(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return &repository.AppAccessToken{
		ID:        row.ID,
		Token:     row.Token,
		ExpiresAt: row.ExpiresAt,
		CreatedAt: row.CreatedAt,
	}, nil
}

func (a *PGAdapter) CreateAppToken(ctx context.Context, token string, expiresAt time.Time) (*repository.AppAccessToken, error) {
	row, err := a.queries.CreateAppToken(ctx, pggen.CreateAppTokenParams{
		Token:     token,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("pg create app token: %w", err)
	}
	return &repository.AppAccessToken{
		ID:        row.ID,
		Token:     row.Token,
		ExpiresAt: row.ExpiresAt,
		CreatedAt: row.CreatedAt,
	}, nil
}

func (a *PGAdapter) DeleteExpiredAppTokens(ctx context.Context) error {
	return a.queries.DeleteExpiredAppTokens(ctx)
}

// Whitelist

func (a *PGAdapter) IsWhitelisted(ctx context.Context, twitchUserID string) (bool, error) {
	return a.queries.IsWhitelisted(ctx, twitchUserID)
}

func (a *PGAdapter) AddToWhitelist(ctx context.Context, twitchUserID string) error {
	return a.queries.AddToWhitelist(ctx, twitchUserID)
}

func (a *PGAdapter) RemoveFromWhitelist(ctx context.Context, twitchUserID string) error {
	return a.queries.RemoveFromWhitelist(ctx, twitchUserID)
}

func (a *PGAdapter) ListWhitelist(ctx context.Context) ([]repository.WhitelistEntry, error) {
	rows, err := a.queries.ListWhitelist(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg list whitelist: %w", err)
	}
	entries := make([]repository.WhitelistEntry, len(rows))
	for i, row := range rows {
		entries[i] = repository.WhitelistEntry{
			TwitchUserID: row.TwitchUserID,
			AddedAt:      row.AddedAt,
		}
	}
	return entries, nil
}
