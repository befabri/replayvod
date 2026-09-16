package pgadapter

import (
	"context"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) BeginMediaPublication(ctx context.Context, input repository.MediaPublication) (*repository.MediaPublication, error) {
	row, err := a.queries.BeginMediaPublication(ctx, pggen.BeginMediaPublicationParams{Key: input.Key, VideoID: input.VideoID, Digest: input.Digest, SizeBytes: input.SizeBytes})
	if err != nil {
		return nil, mapErr(err)
	}
	return pgMediaPublicationToDomain(row), nil
}

func (a *PGAdapter) ListMediaPublications(ctx context.Context, after string, limit int) ([]repository.MediaPublication, error) {
	rows, err := a.queries.ListMediaPublications(ctx, pggen.ListMediaPublicationsParams{After: after, Limit: int32(limit)})
	if err != nil {
		return nil, err
	}
	return pgMediaPublicationsToDomain(rows), nil
}
func (a *PGAdapter) ListRecordingPublications(ctx context.Context, videoID int64, after string, limit int) ([]repository.MediaPublication, error) {
	rows, err := a.queries.ListRecordingPublications(ctx, pggen.ListRecordingPublicationsParams{VideoID: videoID, After: after, Limit: int32(limit)})
	if err != nil {
		return nil, err
	}
	return pgMediaPublicationsToDomain(rows), nil
}
