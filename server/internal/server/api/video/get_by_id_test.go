package video

import (
	"context"
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/trpcgo"
)

type failingPartsRepo struct {
	repository.Repository
	partsErr error
}

func (r *failingPartsRepo) ListVideoParts(ctx context.Context, id int64) ([]repository.VideoPart, error) {
	if r.partsErr != nil {
		return nil, r.partsErr
	}
	return r.Repository.ListVideoParts(ctx, id)
}

func TestGetByIDDoesNotCacheMissingPartsAfterReadFailure(t *testing.T) {
	ctx := t.Context()
	repo := &failingPartsRepo{Repository: sqliteadapter.New(testdb.NewSQLiteDB(t))}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "parts", BroadcasterLogin: "parts", BroadcasterName: "Parts"}); err != nil {
		t.Fatal(err)
	}
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{JobID: "parts", Filename: "parts", BroadcasterID: "parts", Status: repository.VideoStatusDone, Quality: repository.QualityHigh})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{VideoID: v.ID, PartIndex: 1, Filename: "parts-01.ts", Codec: repository.CodecH264, SegmentFormat: "ts"}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{video: New(repo, testClientLogger()), log: testClientLogger()}
	repo.partsErr = errors.New("parts query unavailable")
	_, err = h.GetByID(ctx, GetByIDInput{ID: v.ID})
	var rpcErr *trpcgo.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != trpcgo.CodeInternalServerError {
		t.Fatalf("parts read failure returned a successful snapshot: %v", err)
	}
	repo.partsErr = nil
	got, err := h.GetByID(ctx, GetByIDInput{ID: v.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Parts) != 1 || got.Parts[0].Filename != "parts-01.ts" {
		t.Fatalf("retry lost authoritative part reference: %+v", got.Parts)
	}
}
