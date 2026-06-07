package sqliteadapter

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/contracttest"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitetype"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// TestContract runs the backend-agnostic repository contract against the SQLite
// adapter. The shared suite lives in contracttest; only the fresh-DB factory
// and the backend-specific setup hooks live here.
func TestContract(t *testing.T) {
	contracttest.Run(t, func(t *testing.T) contracttest.Harness {
		db := testdb.NewSQLiteDB(t)
		return &contractHarness{a: New(db), db: db}
	})
}

type contractHarness struct {
	a  *SQLiteAdapter
	db *sql.DB
}

func (h *contractHarness) Repo() repository.Repository { return h.a }

func (h *contractHarness) BackdateAllSubscriptionsCreated(t *testing.T, at time.Time) {
	t.Helper()
	if _, err := h.db.ExecContext(context.Background(), "UPDATE subscriptions SET created_at = ?", sqlitetype.Format(at)); err != nil {
		t.Fatalf("backdate subscriptions created_at: %v", err)
	}
}

func (h *contractHarness) BackdateVideoStartDownload(t *testing.T, videoID int64, at time.Time) {
	t.Helper()
	if _, err := h.db.ExecContext(context.Background(), "UPDATE videos SET start_download_at = ? WHERE id = ?", sqlitetype.Format(at), videoID); err != nil {
		t.Fatalf("backdate video start_download_at: %v", err)
	}
}

func (h *contractHarness) BackdateVideoDownloadedAt(t *testing.T, videoID int64, at time.Time) {
	t.Helper()
	if _, err := h.db.ExecContext(context.Background(), "UPDATE videos SET downloaded_at = ? WHERE id = ?", sqlitetype.Format(at), videoID); err != nil {
		t.Fatalf("backdate video downloaded_at: %v", err)
	}
}

func (h *contractHarness) BackdateVideoDeletedAt(t *testing.T, videoID int64, at time.Time) {
	t.Helper()
	if _, err := h.db.ExecContext(context.Background(), "UPDATE videos SET deleted_at = ? WHERE id = ?", sqlitetype.Format(at), videoID); err != nil {
		t.Fatalf("backdate video deleted_at: %v", err)
	}
}

func (h *contractHarness) BackdateVideoUserStateWatched(t *testing.T, userID string, videoID int64, at time.Time) {
	t.Helper()
	if _, err := h.db.ExecContext(context.Background(), "UPDATE video_user_states SET watched_at = ? WHERE user_id = ? AND video_id = ?", sqlitetype.Format(at), userID, videoID); err != nil {
		t.Fatalf("backdate video_user_states watched_at: %v", err)
	}
}

func (h *contractHarness) BackdateRecordingWebhookDelivery(t *testing.T, id int64, createdAt, updatedAt, deliveredAt *time.Time) {
	t.Helper()
	sets := make([]string, 0, 3)
	args := make([]any, 0, 4)
	add := func(col string, v *time.Time) {
		if v == nil {
			return
		}
		sets = append(sets, col+" = ?")
		args = append(args, sqlitetype.Format(*v))
	}
	add("created_at", createdAt)
	add("updated_at", updatedAt)
	add("delivered_at", deliveredAt)
	if len(sets) == 0 {
		return
	}
	args = append(args, id)
	q := "UPDATE recording_webhook_deliveries SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	if _, err := h.db.ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("backdate recording webhook delivery: %v", err)
	}
}
