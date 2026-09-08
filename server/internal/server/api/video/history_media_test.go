package video

import (
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
)

func TestHistoryReportsFailedMediaFromParts(t *testing.T) {
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	ctx := t.Context()
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "bc", BroadcasterLogin: "channel", BroadcasterName: "Channel"}); err != nil {
		t.Fatal(err)
	}
	var rows []repository.Video
	for _, name := range []string{"empty-failure", "captured-failure"} {
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{JobID: name, Filename: name, BroadcasterID: "bc", DisplayName: "Channel", Status: repository.VideoStatusFailed, Quality: repository.QualityHigh, RecordingType: repository.RecordingTypeVideo})
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, *v)
	}
	if _, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{VideoID: rows[1].ID, PartIndex: 1, Filename: "captured.mp4", Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatTS}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{video: New(repo, testClientLogger()), log: testClientLogger()}
	responses := h.toVideoResponses(ctx, "", rows)
	if responses[0].HasMedia || !responses[1].HasMedia {
		t.Fatalf("failed media classification: %+v", responses)
	}
	if err := repo.SoftDeleteVideo(ctx, rows[1].ID, repository.DeletionKindManual); err != nil {
		t.Fatal(err)
	}
	removed, err := repo.GetVideo(ctx, rows[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if h.toVideoResponses(ctx, "", []repository.Video{*removed})[0].HasMedia {
		t.Fatal("removed recording reports present media")
	}
}
