package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) GetVideoUserState(ctx context.Context, userID string, videoID int64) (*repository.VideoUserState, error) {
	row, err := a.queries.GetVideoUserState(ctx, pggen.GetVideoUserStateParams{
		UserID:  userID,
		VideoID: videoID,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return pgVideoUserStateToDomain(row), nil
}

func (a *PGAdapter) ListVideoUserStatesForVideos(ctx context.Context, userID string, videoIDs []int64) ([]repository.VideoUserState, error) {
	if userID == "" || len(videoIDs) == 0 {
		return []repository.VideoUserState{}, nil
	}
	rows, err := a.queries.ListVideoUserStatesForVideos(ctx, pggen.ListVideoUserStatesForVideosParams{
		UserID:   userID,
		VideoIds: videoIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("pg list video user states: %w", err)
	}
	out := make([]repository.VideoUserState, len(rows))
	for i, row := range rows {
		out[i] = *pgVideoUserStateToDomain(row)
	}
	return out, nil
}

func (a *PGAdapter) SetVideoWatchLater(ctx context.Context, userID string, videoID int64, watchLater bool) (*repository.VideoUserState, error) {
	row, err := a.queries.SetVideoWatchLater(ctx, pggen.SetVideoWatchLaterParams{
		UserID:     userID,
		VideoID:    videoID,
		WatchLater: watchLater,
	})
	if err != nil {
		return nil, fmt.Errorf("pg set video watch later: %w", err)
	}
	return pgVideoUserStateToDomain(row), nil
}

func (a *PGAdapter) UpdateVideoWatchProgress(ctx context.Context, userID string, videoID int64, positionSeconds float64, completed bool, observedAtMs int64) (*repository.VideoUserState, error) {
	row, err := a.queries.UpdateVideoWatchProgress(ctx, pggen.UpdateVideoWatchProgressParams{
		UserID:          userID,
		ID:              videoID,
		PositionSeconds: positionSeconds,
		ObservedAtMs:    observedAtMs,
		Completed:       completed,
	})
	if err != nil {
		return nil, fmt.Errorf("pg update video watch progress: %w", mapErr(err))
	}
	return pgVideoUserStateToDomain(row), nil
}
