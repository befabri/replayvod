package contracttest

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// The frontend runs these same boundary cases through resumeOffsetSeconds.
//
//go:embed testdata/resume-policy.json
var resumePolicyCases []byte

func testContinueWatchingPolicyAndOrder(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	for _, id := range []string{"resume-viewer", "resume-other"} {
		if _, err := repo.UpsertUser(ctx, &repository.User{ID: id, Login: id, DisplayName: id, Role: "viewer"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "resume-channel", BroadcasterLogin: "resume-channel", BroadcasterName: "Resume"}); err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string   `json:"name"`
		Duration *float64 `json:"duration"`
		Position float64  `json:"position"`
		Resume   bool     `json:"resume"`
	}
	if err := json.Unmarshal(resumePolicyCases, &cases); err != nil {
		t.Fatal(err)
	}
	want := []string{}
	for i, tc := range cases {
		jobID := fmt.Sprintf("resume-%02d", i)
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{JobID: jobID, Filename: jobID, DisplayName: tc.Name, Status: repository.VideoStatusDone, Quality: repository.QualityHigh, BroadcasterID: "resume-channel", Language: "en", RecordingType: repository.RecordingTypeVideo})
		if err != nil {
			t.Fatal(err)
		}
		if tc.Duration != nil {
			if err := repo.MarkVideoDone(ctx, v.ID, *tc.Duration, 1, nil, repository.CompletionKindComplete, false); err != nil {
				t.Fatal(err)
			}
		}
		// Newer recordings are deliberately watched earlier. Pairs share a
		// progress timestamp to exercise the ID tie-break across page boundaries.
		stamp := int64(100_000 - i/2*1000)
		// completed_at is historical: a rewatch must still resume by position.
		if _, err := repo.UpdateVideoWatchProgress(ctx, "resume-viewer", v.ID, 0, true, time.UnixMilli(stamp-1)); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.UpdateVideoWatchProgress(ctx, "resume-viewer", v.ID, tc.Position, false, time.UnixMilli(stamp)); err != nil {
			t.Fatal(err)
		}
		// A different viewer's recent activity must not reorder this viewer's list.
		if _, err := repo.UpdateVideoWatchProgress(ctx, "resume-other", v.ID, 60, false, time.UnixMilli(200_000+int64(i))); err != nil {
			t.Fatal(err)
		}
		if tc.Resume {
			want = append(want, jobID)
		}
	}
	slices.SortFunc(want, func(a, b string) int {
		var ai, bi int
		fmt.Sscanf(a, "resume-%d", &ai)
		fmt.Sscanf(b, "resume-%d", &bi)
		if ai/2 != bi/2 {
			return ai/2 - bi/2
		}
		return bi - ai
	})
	opts := repository.ListVideosOpts{UserID: "resume-viewer", ContinueWatchingOnly: true, Sort: "last_watched", Order: "desc", Limit: 1}
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, repo, opts), want)
	assertStringSlice(t, continueWatchingJobIDs(t, ctx, repo, "resume-viewer", len(cases)), want)
	totals, err := repo.VideoStatsTotals(ctx, "resume-viewer")
	if err != nil {
		t.Fatal(err)
	}
	if totals.ContinueWatching != int64(len(want)) {
		t.Fatalf("count = %d, want %d", totals.ContinueWatching, len(want))
	}
	opts.Order = "asc"
	slices.Reverse(want)
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, repo, opts), want)
	// Unstarted rows sort last in either direction when sorting the full library.
	opts.UserID = ""
	opts.ContinueWatchingOnly = false
	opts.Limit = 2
	if got := collectVideoListPageJobIDs(t, ctx, repo, opts); len(got) != len(cases) {
		t.Fatalf("null progress pagination returned %d rows", len(got))
	}
}
