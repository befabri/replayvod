package pgadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) GetStream(ctx context.Context, id string) (*repository.Stream, error) {
	row, err := a.queries.GetStream(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgStreamToDomain(row), nil
}

func (a *PGAdapter) UpsertStream(ctx context.Context, s *repository.StreamInput) (*repository.Stream, error) {
	row, err := a.queries.UpsertStream(ctx, pggen.UpsertStreamParams{
		ID:            s.ID,
		BroadcasterID: s.BroadcasterID,
		Type:          s.Type,
		Language:      s.Language,
		ThumbnailUrl:  s.ThumbnailURL,
		ViewerCount:   int32(s.ViewerCount),
		IsMature:      s.IsMature,
		StartedAt:     s.StartedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("pg upsert stream %s: %w", s.ID, err)
	}
	return pgStreamToDomain(row), nil
}

func (a *PGAdapter) EndStream(ctx context.Context, id string, endedAt time.Time) error {
	return a.queries.EndStream(ctx, pggen.EndStreamParams{
		ID:      id,
		EndedAt: &endedAt,
	})
}

func (a *PGAdapter) UpdateStreamViewers(ctx context.Context, id string, viewerCount int64) error {
	return a.queries.UpdateStreamViewers(ctx, pggen.UpdateStreamViewersParams{
		ID:          id,
		ViewerCount: int32(viewerCount),
	})
}

func (a *PGAdapter) ListActiveStreams(ctx context.Context) ([]repository.Stream, error) {
	rows, err := a.queries.ListActiveStreams(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg list active streams: %w", err)
	}
	return pgStreamsToDomain(rows), nil
}

func (a *PGAdapter) ListStreamsByBroadcaster(ctx context.Context, broadcasterID string, limit, offset int) ([]repository.Stream, error) {
	rows, err := a.queries.ListStreamsByBroadcaster(ctx, pggen.ListStreamsByBroadcasterParams{
		BroadcasterID: broadcasterID,
		Limit:         int32(limit),
		Offset:        int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list streams by broadcaster: %w", err)
	}
	return pgStreamsToDomain(rows), nil
}

func (a *PGAdapter) GetLastLiveStream(ctx context.Context, broadcasterID string) (*repository.Stream, error) {
	row, err := a.queries.GetLastLiveStream(ctx, broadcasterID)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgStreamToDomain(row), nil
}

func (a *PGAdapter) ListLatestLivePerChannel(ctx context.Context, limit int) ([]repository.LatestLiveStream, error) {
	rows, err := a.queries.ListLatestLivePerChannel(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("pg list latest live per channel: %w", err)
	}
	out := make([]repository.LatestLiveStream, len(rows))
	for i, r := range rows {
		out[i] = repository.LatestLiveStream{
			Stream: repository.Stream{
				ID:            r.ID,
				BroadcasterID: r.BroadcasterID,
				Type:          r.Type,
				Language:      r.Language,
				ThumbnailURL:  r.ThumbnailUrl,
				ViewerCount:   int64(r.ViewerCount),
				IsMature:      r.IsMature,
				StartedAt:     r.StartedAt,
				EndedAt:       r.EndedAt,
				CreatedAt:     r.CreatedAt,
			},
			BroadcasterLogin: r.BroadcasterLogin,
			BroadcasterName:  r.BroadcasterName,
			ProfileImageURL:  r.ProfileImageUrl,
		}
	}
	return out, nil
}

func pgStreamToDomain(s pggen.Stream) *repository.Stream {
	return &repository.Stream{
		ID:            s.ID,
		BroadcasterID: s.BroadcasterID,
		Type:          s.Type,
		Language:      s.Language,
		ThumbnailURL:  s.ThumbnailUrl,
		ViewerCount:   int64(s.ViewerCount),
		IsMature:      s.IsMature,
		StartedAt:     s.StartedAt,
		EndedAt:       s.EndedAt,
		CreatedAt:     s.CreatedAt,
	}
}

func pgStreamsToDomain(rows []pggen.Stream) []repository.Stream {
	out := make([]repository.Stream, len(rows))
	for i, r := range rows {
		out[i] = *pgStreamToDomain(r)
	}
	return out
}
