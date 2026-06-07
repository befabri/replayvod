package sqliteadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) GetVideoUserState(ctx context.Context, userID string, videoID int64) (*repository.VideoUserState, error) {
	row, err := a.queries.GetVideoUserState(ctx, sqlitegen.GetVideoUserStateParams{
		UserID:  userID,
		VideoID: videoID,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteVideoUserStateToDomain(row), nil
}

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

func (a *SQLiteAdapter) SetVideoWatchLater(ctx context.Context, userID string, videoID int64, watchLater bool) (*repository.VideoUserState, error) {
	row, err := a.queries.SetVideoWatchLater(ctx, sqlitegen.SetVideoWatchLaterParams{
		UserID:     userID,
		VideoID:    videoID,
		WatchLater: boolToInt64(watchLater),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite set video watch later: %w", err)
	}
	return sqliteVideoUserStateToDomain(row), nil
}

func (a *SQLiteAdapter) UpdateVideoWatchProgress(ctx context.Context, userID string, videoID int64, positionSeconds float64, completed bool, observedAtMs int64) (*repository.VideoUserState, error) {
	row, err := a.queries.UpdateVideoWatchProgress(ctx, sqlitegen.UpdateVideoWatchProgressParams{
		UserID:          userID,
		PositionSeconds: positionSeconds,
		ObservedAtMs:    observedAtMs,
		Completed:       boolToInt64(completed),
		ID:              videoID,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite update video watch progress: %w", mapErr(err))
	}
	return sqliteVideoUserStateToDomain(row), nil
}

func sqliteVideoUserStateToDomain(row sqlitegen.VideoUserState) *repository.VideoUserState {
	return &repository.VideoUserState{
		UserID:              row.UserID,
		VideoID:             row.VideoID,
		WatchLater:          row.WatchLater != 0,
		LastPositionSeconds: row.LastPositionSeconds,
		LastProgressAtMs:    int64PtrFromSQLite(row.LastProgressAtMs),
		WatchedAt:           timePtrFromSQLite(row.WatchedAt),
		CompletedAt:         timePtrFromSQLite(row.CompletedAt),
		CreatedAt:           row.CreatedAt.Time,
		UpdatedAt:           row.UpdatedAt.Time,
	}
}
