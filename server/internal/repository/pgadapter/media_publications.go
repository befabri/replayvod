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
