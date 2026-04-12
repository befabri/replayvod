package sqliteadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) LinkStreamCategory(ctx context.Context, streamID, categoryID string) error {
	return a.queries.LinkStreamCategory(ctx, sqlitegen.LinkStreamCategoryParams{StreamID: streamID, CategoryID: categoryID})
}

func (a *SQLiteAdapter) LinkVideoCategory(ctx context.Context, videoID int64, categoryID string) error {
	return a.queries.LinkVideoCategory(ctx, sqlitegen.LinkVideoCategoryParams{VideoID: videoID, CategoryID: categoryID})
}

func (a *SQLiteAdapter) LinkStreamTag(ctx context.Context, streamID string, tagID int64) error {
	return a.queries.LinkStreamTag(ctx, sqlitegen.LinkStreamTagParams{StreamID: streamID, TagID: tagID})
}

func (a *SQLiteAdapter) LinkVideoTag(ctx context.Context, videoID, tagID int64) error {
	return a.queries.LinkVideoTag(ctx, sqlitegen.LinkVideoTagParams{VideoID: videoID, TagID: tagID})
}

func (a *SQLiteAdapter) ListCategoriesForVideo(ctx context.Context, videoID int64) ([]repository.Category, error) {
	rows, err := a.queries.ListCategoriesForVideo(ctx, videoID)
	if err != nil {
		return nil, fmt.Errorf("sqlite list categories for video: %w", err)
	}
	out := make([]repository.Category, len(rows))
	for i, r := range rows {
		out[i] = *sqliteCategoryToDomain(r)
	}
	return out, nil
}

func (a *SQLiteAdapter) ListTagsForVideo(ctx context.Context, videoID int64) ([]repository.Tag, error) {
	rows, err := a.queries.ListTagsForVideo(ctx, videoID)
	if err != nil {
		return nil, fmt.Errorf("sqlite list tags for video: %w", err)
	}
	out := make([]repository.Tag, len(rows))
	for i, r := range rows {
		out[i] = *sqliteTagToDomain(r)
	}
	return out, nil
}

func (a *SQLiteAdapter) AddVideoRequest(ctx context.Context, videoID int64, userID string) error {
	return a.queries.AddVideoRequest(ctx, sqlitegen.AddVideoRequestParams{VideoID: videoID, UserID: userID})
}

func (a *SQLiteAdapter) ListVideoRequestsForUser(ctx context.Context, userID string, limit, offset int) ([]repository.Video, error) {
	rows, err := a.queries.ListVideoRequestsForUser(ctx, sqlitegen.ListVideoRequestsForUserParams{
		UserID: userID,
		Limit:  int64(limit),
		Offset: int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list video requests: %w", err)
	}
	return sqliteVideosToDomain(rows), nil
}
