package pgadapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.ErrNotFound
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" { // unique_violation
		return repository.ErrDuplicate
	}
	return err
}

// PGAdapter implements repository.Repository with PostgreSQL.
type PGAdapter struct {
	queries *pggen.Queries
	db      pggen.DBTX
}

var _ repository.Repository = (*PGAdapter)(nil)

// New returns an adapter backed by db; transactions require a pool or transaction.
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

type pgBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// inTx joins an existing transaction; otherwise it owns commit and rollback,
// including rollback on panic.
func (a *PGAdapter) inTx(ctx context.Context, fn func(q *pggen.Queries, tx pgx.Tx) error) error {
	if tx, ok := a.db.(pgx.Tx); ok {
		return fn(a.queries, tx)
	}
	beginner, ok := a.db.(pgBeginner)
	if !ok {
		return fmt.Errorf("pg adapter: underlying db does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("pg begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	if err := fn(a.queries.WithTx(tx), tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: %w", repository.ErrCommitUncertain, err)
	}
	return nil
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
