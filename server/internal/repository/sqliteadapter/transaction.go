package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return a.inTx(ctx, func(_ *sqlitegen.Queries, tx *sql.Tx) error {
		return fn(New(tx))
	})
}

func (a *SQLiteAdapter) GetUserForUpdate(ctx context.Context, id string) (*repository.User, error) {
	if _, ok := a.db.(*sql.Tx); !ok {
		return nil, fmt.Errorf("sqlite get user for update: requires a transaction")
	}
	row, err := a.queries.GetUserForUpdate(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("sqlite lock user %s: %w", id, mapErr(err))
	}
	return sqliteUserToDomain(row), nil
}
