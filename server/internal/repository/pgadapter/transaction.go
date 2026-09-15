package pgadapter

import (
	"context"
	"errors"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
	"github.com/jackc/pgx/v5"
)

func (a *PGAdapter) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	if _, ok := a.db.(pgx.Tx); ok {
		return fmt.Errorf("pg adapter: nested WithTx is unsupported")
	}
	return a.inTx(ctx, func(_ *pggen.Queries, tx pgx.Tx) error {
		return fn(New(tx))
	})
}

// inTransaction reports whether the adapter runs on a WithTx transaction.
func (a *PGAdapter) inTransaction() bool {
	_, ok := a.db.(pgx.Tx)
	return ok
}

func (a *PGAdapter) GetUserForUpdate(ctx context.Context, id string) (*repository.User, error) {
	tx, ok := a.db.(pgx.Tx)
	if !ok {
		return nil, repository.ErrNoTransaction
	}
	row, err := a.queries.GetUserForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		// Absent users require a table lock to block signup. Recheck
		// afterward because signup may have committed while the lock
		// was pending.
		if _, err := tx.Exec(ctx, "LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE"); err != nil {
			return nil, fmt.Errorf("pg lock missing user %s: %w", id, err)
		}
		row, err = a.queries.GetUserForUpdate(ctx, id)
	}
	if err != nil {
		return nil, fmt.Errorf("pg lock user %s: %w", id, mapErr(err))
	}
	return pgUserToDomain(row), nil
}
