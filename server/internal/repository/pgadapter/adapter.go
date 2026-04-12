package pgadapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

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
type PGAdapter struct {
	queries *pggen.Queries
}

// New creates a new PGAdapter.
func New(queries *pggen.Queries) *PGAdapter {
	return &PGAdapter{queries: queries}
}

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
		Email:           toPgText(u.Email),
		ProfileImageUrl: toPgText(u.ProfileImageURL),
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
	if err := a.queries.UpdateUserRole(ctx, pggen.UpdateUserRoleParams{
		ID:   id,
		Role: role,
	}); err != nil {
		return fmt.Errorf("pg update user role %s: %w", id, err)
	}
	return nil
}

// Conversion helpers

func pgUserToDomain(u pggen.User) *repository.User {
	return &repository.User{
		ID:              u.ID,
		Login:           u.Login,
		DisplayName:     u.DisplayName,
		Email:           fromPgText(u.Email),
		ProfileImageURL: fromPgText(u.ProfileImageUrl),
		Role:            u.Role,
		CreatedAt:       u.CreatedAt.Time,
		UpdatedAt:       u.UpdatedAt.Time,
	}
}

func fromPgText(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func toPgText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func toPgTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// Sessions

func (a *PGAdapter) CreateSession(ctx context.Context, s *repository.Session) error {
	if err := a.queries.CreateSession(ctx, pggen.CreateSessionParams{
		HashedID:        s.HashedID,
		UserID:          s.UserID,
		EncryptedTokens: s.EncryptedTokens,
		ExpiresAt:       toPgTimestamptz(s.ExpiresAt),
		UserAgent:       toPgText(s.UserAgent),
		IpAddress:       toPgText(s.IPAddress),
	}); err != nil {
		return fmt.Errorf("pg create session: %w", err)
	}
	return nil
}

func (a *PGAdapter) GetSession(ctx context.Context, hashedID string) (*repository.Session, error) {
	row, err := a.queries.GetSession(ctx, hashedID)
	if err != nil {
		return nil, fmt.Errorf("pg get session: %w", err)
	}
	return &repository.Session{
		HashedID:        row.HashedID,
		UserID:          row.UserID,
		EncryptedTokens: row.EncryptedTokens,
		ExpiresAt:       row.ExpiresAt.Time,
		LastActiveAt:    row.LastActiveAt.Time,
		UserAgent:       fromPgText(row.UserAgent),
		IPAddress:       fromPgText(row.IpAddress),
		CreatedAt:       row.CreatedAt.Time,
	}, nil
}

func (a *PGAdapter) UpdateSessionTokens(ctx context.Context, hashedID string, encryptedTokens []byte) error {
	if err := a.queries.UpdateSessionTokens(ctx, pggen.UpdateSessionTokensParams{
		HashedID:        hashedID,
		EncryptedTokens: encryptedTokens,
	}); err != nil {
		return fmt.Errorf("pg update session tokens: %w", err)
	}
	return nil
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
			ExpiresAt:    row.ExpiresAt.Time,
			LastActiveAt: row.LastActiveAt.Time,
			UserAgent:    fromPgText(row.UserAgent),
			IPAddress:    fromPgText(row.IpAddress),
			CreatedAt:    row.CreatedAt.Time,
		}
	}
	return sessions, nil
}

// App Access Tokens

func (a *PGAdapter) GetLatestAppToken(ctx context.Context) (*repository.AppAccessToken, error) {
	row, err := a.queries.GetLatestAppToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg get latest app token: %w", err)
	}
	return &repository.AppAccessToken{
		ID:        row.ID,
		Token:     row.Token,
		ExpiresAt: row.ExpiresAt.Time,
		CreatedAt: row.CreatedAt.Time,
	}, nil
}

func (a *PGAdapter) CreateAppToken(ctx context.Context, token string, expiresAt time.Time) (*repository.AppAccessToken, error) {
	row, err := a.queries.CreateAppToken(ctx, pggen.CreateAppTokenParams{
		Token:     token,
		ExpiresAt: toPgTimestamptz(expiresAt),
	})
	if err != nil {
		return nil, fmt.Errorf("pg create app token: %w", err)
	}
	return &repository.AppAccessToken{
		ID:        row.ID,
		Token:     row.Token,
		ExpiresAt: row.ExpiresAt.Time,
		CreatedAt: row.CreatedAt.Time,
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
			AddedAt:      row.AddedAt.Time,
		}
	}
	return entries, nil
}
