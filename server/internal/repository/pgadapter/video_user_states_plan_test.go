package pgadapter

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/contracttest"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// queryRecorder lets EXPLAIN inspect the adapter's actual query and arguments.
type queryRecorder struct {
	sql  string
	args []any
}

func (r *queryRecorder) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	r.sql, r.args = data.SQL, data.Args
	return ctx
}

func (r *queryRecorder) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

type planNode struct {
	NodeType  string     `json:"Node Type"`
	IndexName string     `json:"Index Name"`
	Plans     []planNode `json:"Plans"`
}

func (n planNode) walk(visit func(planNode)) {
	visit(n)
	for _, child := range n.Plans {
		child.walk(visit)
	}
}

// TestListContinueWatchingVideos_UsesProgressIndex guards against sorting the
// viewer's entire history before applying the page limit.
func TestListContinueWatchingVideos_UsesProgressIndex(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPGPool(t)
	a := New(pool)
	contracttest.SeedUserChannel(t, ctx, a, "u-plan", "b-plan")

	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := range 400 {
		jobID := fmt.Sprintf("plan-%03d", i)
		v, err := a.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: "b-plan",
			Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
			BroadcasterID: "b-plan", RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("CreateVideo %s: %v", jobID, err)
		}
		if err := a.MarkVideoDone(ctx, v.ID, 3600, 1024, nil, repository.CompletionKindComplete, false); err != nil {
			t.Fatalf("MarkVideoDone %s: %v", jobID, err)
		}
		if _, err := a.UpdateVideoWatchProgress(ctx, "u-plan", v.ID, 600, false, base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("UpdateVideoWatchProgress %s: %v", jobID, err)
		}
	}
	if _, err := pool.Exec(ctx, "ANALYZE videos, video_user_states"); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	rec := &queryRecorder{}
	cfg, err := pgxpool.ParseConfig(pool.Config().ConnString())
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	cfg.ConnConfig.Tracer = rec
	traced, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open traced pool: %v", err)
	}
	defer traced.Close()

	for _, order := range []string{"desc", "asc"} {
		t.Run(order, func(t *testing.T) {
			page, err := New(traced).ListVideosPage(ctx, repository.ListVideosOpts{
				UserID: "u-plan", ContinueWatchingOnly: true, Sort: "last_watched", Order: order, Limit: 12,
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 12 {
				t.Fatalf("ListVideosPage = %d rows; want 12", len(page.Items))
			}
			if rec.sql == "" {
				t.Fatal("tracer captured no query")
			}

			var explained []struct {
				Plan planNode `json:"Plan"`
			}
			if err := pool.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+rec.sql, rec.args...).Scan(&explained); err != nil {
				t.Fatalf("explain: %v", err)
			}
			if len(explained) != 1 {
				t.Fatalf("explain returned %d plans, want 1", len(explained))
			}
			var nodeTypes []string
			usesIndex := false
			wantIndex := "idx_video_user_states_progress"
			if order == "asc" {
				wantIndex += "_asc"
			}
			explained[0].Plan.walk(func(n planNode) {
				nodeTypes = append(nodeTypes, n.NodeType)
				if n.IndexName == wantIndex {
					usesIndex = true
				}
			})
			if !usesIndex {
				t.Fatalf("plan does not read %s: %v", wantIndex, nodeTypes)
			}
			for _, nt := range nodeTypes {
				if nt == "Sort" {
					t.Fatalf("plan sorts instead of reading the index in order: %v", nodeTypes)
				}
			}
			t.Logf("plan nodes: %v", nodeTypes)
		})
	}
}
