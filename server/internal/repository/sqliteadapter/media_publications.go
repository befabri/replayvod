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
