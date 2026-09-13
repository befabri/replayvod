package video

import (
	"reflect"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/testdb"
)

func TestSnapshotListingContinuesPastJournaledUploadHoles(t *testing.T) {
	ctx := t.Context()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "snapshots", BroadcasterLogin: "snapshots", BroadcasterName: "Snapshots"}); err != nil {
		t.Fatal(err)
	}
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{JobID: "snapshots", Filename: "snapshots", BroadcasterID: "snapshots", Status: repository.VideoStatusDone, Quality: repository.QualityHigh})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		key := storagekeys.Snapshot(v.Filename, i)
		if _, err := repo.BeginMediaPublication(ctx, repository.MediaPublication{Key: key, VideoID: v.ID, Digest: "frame"}); err != nil {
			t.Fatal(err)
		}
		if i != 1 {
			if err := raw.Save(ctx, key, strings.NewReader("jpeg")); err != nil {
				t.Fatal(err)
			}
		}
	}
	svc := &Service{repo: repo}
	got, err := svc.ListSnapshots(ctx, raw, v.ID)
	want := []string{storagekeys.Snapshot(v.Filename, 0), storagekeys.Snapshot(v.Filename, 2)}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot after failed upload disappeared: got=%v want=%v err=%v", got, want, err)
	}
}
