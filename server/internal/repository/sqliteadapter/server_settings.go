package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (a *SQLiteAdapter) GetServerHMACSecret(ctx context.Context) (string, error) {
	secret, err := a.queries.GetServerHMACSecret(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", mapErr(err)
	}
	return secret, nil
}

func (a *SQLiteAdapter) SetStorageRestoreCursor(ctx context.Context, cursor *int64) error {
	var value sql.NullInt64
	if cursor != nil {
		value.Int64, value.Valid = *cursor, true
	}
	if err := a.queries.SetStorageRestoreCursor(ctx, value); err != nil {
		return fmt.Errorf("sqlite set storage restore cursor: %w", err)
	}
	return nil
}
