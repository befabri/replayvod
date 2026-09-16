package sqliteadapter

import (
	"context"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) BeginMediaPublication(ctx context.Context, input repository.MediaPublication) (*repository.MediaPublication, error) {
	row, err := a.queries.BeginMediaPublication(ctx, sqlitegen.BeginMediaPublicationParams{Key: input.Key, VideoID: input.VideoID, Digest: input.Digest, SizeBytes: input.SizeBytes})
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteMediaPublicationToDomain(row), nil
}

func (a *SQLiteAdapter) ListMediaPublications(ctx context.Context, after string, limit int) ([]repository.MediaPublication, error) {
	rows, err := a.queries.ListMediaPublications(ctx, sqlitegen.ListMediaPublicationsParams{After: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return sqliteMediaPublicationsToDomain(rows), nil
}
func (a *SQLiteAdapter) ListRecordingPublications(ctx context.Context, videoID int64, after string, limit int) ([]repository.MediaPublication, error) {
	rows, err := a.queries.ListRecordingPublications(ctx, sqlitegen.ListRecordingPublicationsParams{VideoID: videoID, After: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return sqliteMediaPublicationsToDomain(rows), nil
}
