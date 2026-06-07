package pgadapter

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/contracttest"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// TestContract runs the backend-agnostic repository contract against the
// Postgres adapter. The shared suite lives in contracttest; only the fresh-DB
// factory and the backend-specific setup hooks live here.
func TestContract(t *testing.T) {
	contracttest.Run(t, func(t *testing.T) contracttest.Harness {
		pool := testdb.NewPGPool(t)
		return &contractHarness{a: New(pool), pool: pool}
	})
}

type contractHarness struct {
	a    *PGAdapter
	pool *pgxpool.Pool
}

func (h *contractHarness) Repo() repository.Repository { return h.a }

func (h *contractHarness) BackdateAllSubscriptionsCreated(t *testing.T, at time.Time) {
	t.Helper()
	if _, err := h.pool.Exec(context.Background(), "UPDATE subscriptions SET created_at = $1", at); err != nil {
		t.Fatalf("backdate subscriptions created_at: %v", err)
	}
}

func (h *contractHarness) BackdateVideoStartDownload(t *testing.T, videoID int64, at time.Time) {
	t.Helper()
	if _, err := h.pool.Exec(context.Background(), "UPDATE videos SET start_download_at = $1 WHERE id = $2", at, videoID); err != nil {
		t.Fatalf("backdate video start_download_at: %v", err)
	}
}

func (h *contractHarness) BackdateVideoDownloadedAt(t *testing.T, videoID int64, at time.Time) {
	t.Helper()
	if _, err := h.pool.Exec(context.Background(), "UPDATE videos SET downloaded_at = $1 WHERE id = $2", at, videoID); err != nil {
		t.Fatalf("backdate video downloaded_at: %v", err)
	}
}

func (h *contractHarness) BackdateVideoDeletedAt(t *testing.T, videoID int64, at time.Time) {
	t.Helper()
	if _, err := h.pool.Exec(context.Background(), "UPDATE videos SET deleted_at = $1 WHERE id = $2", at, videoID); err != nil {
		t.Fatalf("backdate video deleted_at: %v", err)
	}
}

func (h *contractHarness) BackdateVideoUserStateWatched(t *testing.T, userID string, videoID int64, at time.Time) {
	t.Helper()
	if _, err := h.pool.Exec(context.Background(), "UPDATE video_user_states SET watched_at = $1 WHERE user_id = $2 AND video_id = $3", at, userID, videoID); err != nil {
		t.Fatalf("backdate video_user_states watched_at: %v", err)
	}
}

func (h *contractHarness) BackdateRecordingWebhookDelivery(t *testing.T, id int64, createdAt, updatedAt, deliveredAt *time.Time) {
	t.Helper()
	sets := make([]string, 0, 3)
	args := make([]any, 0, 4)
	n := 1
	add := func(col string, v *time.Time) {
		if v == nil {
			return
		}
		sets = append(sets, fmt.Sprintf("%s = $%d", col, n))
		args = append(args, *v)
		n++
	}
	add("created_at", createdAt)
	add("updated_at", updatedAt)
	add("delivered_at", deliveredAt)
	if len(sets) == 0 {
		return
	}
	args = append(args, id)
	q := fmt.Sprintf("UPDATE recording_webhook_deliveries SET %s WHERE id = $%d", strings.Join(sets, ", "), n)
	if _, err := h.pool.Exec(context.Background(), q, args...); err != nil {
		t.Fatalf("backdate recording webhook delivery: %v", err)
	}
}
