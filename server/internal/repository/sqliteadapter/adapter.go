package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitetype"
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
// db is kept alongside queries for ping, transactions, batch upserts, and the
// dynamic ListVideosPage keyset query. Fixed-shape SQL goes through sqlc.
type SQLiteAdapter struct {
	queries *sqlitegen.Queries
	db      sqlitegen.DBTX
}

var _ repository.Repository = (*SQLiteAdapter)(nil)

// New creates a new SQLiteAdapter. db is typically an *sql.DB but any
// sqlitegen.DBTX works; the adapter retains it for the few raw database
// operations that intentionally live outside sqlc.
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

// sqliteBeginner is the minimal surface the adapter needs to open a
// transaction. *sql.DB satisfies it; when db is already a *sql.Tx the
// assertion fails and the caller is expected to be running inside an
// outer transaction already.
type sqliteBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// inTx runs fn inside a database/sql transaction when the adapter's
// underlying DBTX supports opening one. Commits on success, rolls
// back on error or panic.
func (a *SQLiteAdapter) inTx(ctx context.Context, fn func(q *sqlitegen.Queries, tx *sql.Tx) error) error {
	beginner, ok := a.db.(sqliteBeginner)
	if !ok {
		return fmt.Errorf("sqlite adapter: underlying db does not support transactions")
	}
	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()
	if err := fn(a.queries.WithTx(tx), tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite commit tx: %w", err)
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

func fromNullFloat64(f sql.NullFloat64) *float64 {
	if !f.Valid {
		return nil
	}
	return &f.Float64
}

func nullFloat64(f *float64) sql.NullFloat64 {
	if f == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *f, Valid: true}
}

func int64PtrFromSQLite(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func sqliteTime(t time.Time) sqlitetype.Time {
	return sqlitetype.NewTime(t)
}

func sqliteTimePtr(t *time.Time) *sqlitetype.Time {
	if t == nil {
		return nil
	}
	out := sqlitetype.NewTime(*t)
	return &out
}

func timePtrFromSQLite(t *sqlitetype.Time) *time.Time {
	if t == nil {
		return nil
	}
	out := t.Time
	return &out
}

// anyToFloat64 normalises the `interface{}` that sqlc-sqlite emits
// for expressions whose type it can't infer (CASE branches, SUM over
// mixed-shape CASE). modernc.org/sqlite surfaces REAL columns as
// float64 and INTEGER columns as int64; everything else falls back
// to the string form. Zero for unknown shapes.
func anyToFloat64(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case nil:
		return 0
	default:
		return 0
	}
}

// Sessions

func (a *SQLiteAdapter) CreateSession(ctx context.Context, s *repository.Session) error {
	if err := a.queries.CreateSession(ctx, sqlitegen.CreateSessionParams{
		HashedID:        s.HashedID,
		UserID:          s.UserID,
		EncryptedTokens: s.EncryptedTokens,
		ExpiresAt:       sqliteTime(s.ExpiresAt),
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
		ExpiresAt:       row.ExpiresAt.Time,
		LastActiveAt:    row.LastActiveAt.Time,
		UserAgent:       fromNullString(row.UserAgent),
		IPAddress:       fromNullString(row.IpAddress),
		CreatedAt:       row.CreatedAt.Time,
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
			ExpiresAt:    row.ExpiresAt.Time,
			LastActiveAt: row.LastActiveAt.Time,
			UserAgent:    fromNullString(row.UserAgent),
			IPAddress:    fromNullString(row.IpAddress),
			CreatedAt:    row.CreatedAt.Time,
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
		ExpiresAt: row.ExpiresAt.Time,
		CreatedAt: row.CreatedAt.Time,
	}, nil
}

func (a *SQLiteAdapter) CreateAppToken(ctx context.Context, token string, expiresAt time.Time) (*repository.AppAccessToken, error) {
	row, err := a.queries.CreateAppToken(ctx, sqlitegen.CreateAppTokenParams{
		Token:     token,
		ExpiresAt: sqliteTime(expiresAt),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite create app token: %w", err)
	}
	return &repository.AppAccessToken{
		ID:        row.ID,
		Token:     row.Token,
		ExpiresAt: row.ExpiresAt.Time,
		CreatedAt: row.CreatedAt.Time,
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
			AddedAt:      row.AddedAt.Time,
		}
	}
	return entries, nil
}
