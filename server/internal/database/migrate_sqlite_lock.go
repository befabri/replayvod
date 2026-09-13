package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// beginSQLiteMigration waits for a write transaction until ctx ends and restores
// the connection's busy timeout before returning. The caller must discard conn
// on error because cancellation can conceal an acquired transaction.
func beginSQLiteMigration(ctx context.Context, conn *sql.Conn) (err error) {
	var busyTimeout int
	if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		return fmt.Errorf("read busy timeout: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, restoreErr := conn.ExecContext(cleanupCtx, fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeout)); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("restore busy timeout: %w", restoreErr))
		}
	}()
	// SQLite's busy handler can outwait a canceled context; wait in Go instead.
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout = 0"); err != nil {
		return fmt.Errorf("disable busy timeout: %w", err)
	}
	delay := 5 * time.Millisecond
	for {
		_, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE")
		if err == nil {
			return nil
		}
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) || sqliteErr.Code()&0xff != sqlite3.SQLITE_BUSY {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		delay = min(delay*2, 100*time.Millisecond)
	}
}
