package video

import (
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// Library cards and the watch page show a recording's tags, which come from
// the broadcast it recorded.
func TestVideoResponsesCarryTheirBroadcastTags(t *testing.T) {
	ctx := t.Context()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "tags", BroadcasterLogin: "tags", BroadcasterName: "Tags"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertStream(ctx, &repository.StreamInput{ID: "live-1", BroadcasterID: "tags", Type: "live", Language: "en", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"english", "chess"} {
		tag, err := repo.UpsertTag(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.LinkStreamTag(ctx, "live-1", tag.ID); err != nil {
			t.Fatal(err)
		}
	}
	streamID := "live-1"
	recorded, err := repo.CreateVideo(ctx, &repository.VideoInput{JobID: "recorded", Filename: "recorded", BroadcasterID: "tags", StreamID: &streamID, Status: repository.VideoStatusDone, Quality: repository.QualityHigh})
	if err != nil {
		t.Fatal(err)
	}
	archived, err := repo.CreateVideo(ctx, &repository.VideoInput{JobID: "archived", Filename: "archived", BroadcasterID: "tags", Status: repository.VideoStatusDone, Quality: repository.QualityHigh})
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{video: New(repo, testClientLogger()), log: testClientLogger()}

	names := func(tags []VideoTag) []string {
		out := make([]string, len(tags))
		for i, tag := range tags {
			out[i] = tag.Name
		}
		return out
	}
	got, err := h.GetByID(ctx, GetByIDInput{ID: recorded.ID})
	if err != nil {
		t.Fatal(err)
	}
	if n := names(got.Tags); len(n) != 2 || n[0] != "chess" || n[1] != "english" {
		t.Fatalf("watch page tags = %v, want [chess english]", n)
	}
	list, err := h.toVideoResponses(ctx, "", []repository.Video{*recorded, *archived})
	if err != nil {
		t.Fatal(err)
	}
	if n := names(list[0].Tags); len(n) != 2 || n[0] != "chess" {
		t.Fatalf("library card tags = %v, want [chess english]", n)
	}
	if list[1].Tags != nil {
		t.Fatalf("archive of an unseen broadcast has tags %+v", list[1].Tags)
	}
}
