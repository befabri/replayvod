package sqliteadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) ListVideoUserStatesForVideos(ctx context.Context, userID string, videoIDs []int64) ([]repository.VideoUserState, error) {
	if userID == "" || len(videoIDs) == 0 {
		return []repository.VideoUserState{}, nil
	}
	rows, err := a.queries.ListVideoUserStatesForVideos(ctx, sqlitegen.ListVideoUserStatesForVideosParams{
		UserID:   userID,
		VideoIds: videoIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list video user states: %w", err)
	}
	out := make([]repository.VideoUserState, len(rows))
	for i, row := range rows {
		out[i] = *sqliteVideoUserStateToDomain(row)
	}
	return out, nil
}

func (a *SQLiteAdapter) UpdateVideoWatchProgress(ctx context.Context, userID string, videoID int64, positionSeconds float64, completed bool, at time.Time) (*repository.VideoUserState, error) {
	row, err := a.queries.UpdateVideoWatchProgress(ctx, sqlitegen.UpdateVideoWatchProgressParams{
		UserID:          userID,
		PositionSeconds: positionSeconds,
		ProgressAtMs:    at.UnixMilli(),
		Completed:       boolToInt64(completed),
		StartedSeconds:  repository.WatchStartedSeconds,
		StartedFraction: repository.WatchStartedFraction,
		ID:              videoID,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite update video watch progress: %w", mapErr(err))
	}
	return sqliteVideoUserStateToDomain(row), nil
}

func (a *SQLiteAdapter) ListContinueWatchingVideos(ctx context.Context, userID string, limit int) ([]repository.Video, error) {
	page, err := a.ListVideosPage(ctx, repository.ListVideosOpts{
		UserID: userID, Limit: limit, ContinueWatchingOnly: true, Sort: "last_watched", Order: "desc",
	}, nil)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}
