package pgadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) LinkStreamCategory(ctx context.Context, streamID, categoryID string) error {
	return a.queries.LinkStreamCategory(ctx, pggen.LinkStreamCategoryParams{StreamID: streamID, CategoryID: categoryID})
}

func (a *PGAdapter) LinkVideoCategory(ctx context.Context, videoID int64, categoryID string) error {
	return a.queries.LinkVideoCategory(ctx, pggen.LinkVideoCategoryParams{VideoID: videoID, CategoryID: categoryID})
}

func (a *PGAdapter) UpsertVideoCategorySpan(ctx context.Context, videoID int64, categoryID string, at time.Time) error {
	if err := a.queries.UpsertVideoCategorySpan(ctx, pggen.UpsertVideoCategorySpanParams{
		VideoID:    videoID,
		CategoryID: categoryID,
		AtTime:     at.UTC(),
	}); err != nil {
		return fmt.Errorf("pg upsert video category span: %w", err)
	}
	return nil
}

func (a *PGAdapter) LinkStreamTag(ctx context.Context, streamID string, tagID int64) error {
	return a.queries.LinkStreamTag(ctx, pggen.LinkStreamTagParams{StreamID: streamID, TagID: tagID})
}

func (a *PGAdapter) LinkVideoTag(ctx context.Context, videoID, tagID int64) error {
	return a.queries.LinkVideoTag(ctx, pggen.LinkVideoTagParams{VideoID: videoID, TagID: tagID})
}

func (a *PGAdapter) ListCategoriesForVideo(ctx context.Context, videoID int64) ([]repository.CategorySpan, error) {
	rows, err := a.queries.ListCategorySpansForVideo(ctx, videoID)
	if err != nil {
		return nil, fmt.Errorf("pg list category spans for video: %w", err)
	}
	out := make([]repository.CategorySpan, len(rows))
	for i, r := range rows {
		out[i] = repository.CategorySpan{
			Category: repository.Category{
				ID:        r.ID,
				Name:      r.Name,
				BoxArtURL: r.BoxArtUrl,
				IGDBID:    r.IgdbID,
				CreatedAt: r.CreatedAt,
				UpdatedAt: r.UpdatedAt,
			},
			StartedAt:       r.StartedAt,
			EndedAt:         r.EndedAt,
			DurationSeconds: r.DurationSeconds,
		}
	}
	return out, nil
}

func (a *PGAdapter) ListPrimaryCategoriesForVideos(ctx context.Context, videoIDs []int64) (map[int64]repository.Category, error) {
	if len(videoIDs) == 0 {
		return map[int64]repository.Category{}, nil
	}
	rows, err := a.queries.ListPrimaryCategoriesForVideos(ctx, videoIDs)
	if err != nil {
		return nil, fmt.Errorf("pg list primary categories for videos: %w", err)
	}
	out := make(map[int64]repository.Category, len(videoIDs))
	for _, r := range rows {
		out[r.VideoID] = repository.Category{
			ID:        r.ID,
			Name:      r.Name,
			BoxArtURL: r.BoxArtUrl,
			IGDBID:    r.IgdbID,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
		}
	}
	return out, nil
}

func (a *PGAdapter) ListTagsForVideo(ctx context.Context, videoID int64) ([]repository.Tag, error) {
	rows, err := a.queries.ListTagsForVideo(ctx, videoID)
	if err != nil {
		return nil, fmt.Errorf("pg list tags for video: %w", err)
	}
	out := make([]repository.Tag, len(rows))
	for i, r := range rows {
		out[i] = *pgTagToDomain(r)
	}
	return out, nil
}

func (a *PGAdapter) AddVideoRequest(ctx context.Context, videoID int64, userID string) error {
	return a.queries.AddVideoRequest(ctx, pggen.AddVideoRequestParams{VideoID: videoID, UserID: userID})
}

func (a *PGAdapter) ListVideoRequestsForUser(ctx context.Context, userID string, limit, offset int) ([]repository.Video, error) {
	rows, err := a.queries.ListVideoRequestsForUser(ctx, pggen.ListVideoRequestsForUserParams{
		UserID: userID,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list video requests: %w", err)
	}
	return pgVideosToDomain(rows), nil
}
