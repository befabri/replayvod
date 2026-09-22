package pgadapter

import (
	"context"
	"errors"
	"fmt"

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
