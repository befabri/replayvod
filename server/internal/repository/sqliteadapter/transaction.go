package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	if _, ok := a.db.(*sql.Tx); ok {
		return fmt.Errorf("sqlite adapter: nested WithTx is unsupported")
	}
	return a.inTx(ctx, func(_ *sqlitegen.Queries, tx *sql.Tx) error {
		return fn(New(tx))
	})
}

// inTransaction reports whether the adapter runs on a WithTx transaction.
func (a *SQLiteAdapter) inTransaction() bool {
	_, ok := a.db.(*sql.Tx)
	return ok
}

func (a *SQLiteAdapter) GetUserForUpdate(ctx context.Context, id string) (*repository.User, error) {
	if !a.inTransaction() {
		return nil, repository.ErrNoTransaction
	}
	row, err := a.queries.GetUserForUpdate(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("sqlite lock user %s: %w", id, mapErr(err))
	}
	return sqliteUserToDomain(row), nil
}
