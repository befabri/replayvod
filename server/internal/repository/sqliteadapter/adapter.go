package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitetype"
)

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return repository.ErrNotFound
	}
	if se, ok := errors.AsType[*sqlite.Error](err); ok {
		switch se.Code() {
		case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
			return repository.ErrDuplicate
		}
	}
	return err
}

// SQLiteAdapter implements repository.Repository with SQLite.
type SQLiteAdapter struct {
	queries *sqlitegen.Queries
	db      sqlitegen.DBTX
}

var _ repository.Repository = (*SQLiteAdapter)(nil)

// New returns an adapter backed by db; transactions require a pool or transaction.
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

// sqliteBeginner excludes *sql.Tx, which cannot open a nested transaction.
type sqliteBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// inTx joins an existing transaction; otherwise it owns commit and rollback,
// including rollback on panic.
func (a *SQLiteAdapter) inTx(ctx context.Context, fn func(q *sqlitegen.Queries, tx *sql.Tx) error) error {
	if tx, ok := a.db.(*sql.Tx); ok {
		return fn(a.queries, tx)
	}
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
		return fmt.Errorf("%w: %w", repository.ErrCommitUncertain, err)
	}
	return nil
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

func sqlitePreciseTimePtr(t *time.Time) *sqlitetype.PreciseTime {
	if t == nil {
		return nil
	}
	out := sqlitetype.NewPreciseTime(*t)
	return &out
}

func timePtrFromSQLitePrecise(t *sqlitetype.PreciseTime) *time.Time {
	if t == nil {
		return nil
	}
	out := t.Time.Time
	return &out
}

// anyToFloat64 accepts SQLite REAL and INTEGER aggregates; NULL and unknown
// representations yield zero.
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

// Whitelist

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
