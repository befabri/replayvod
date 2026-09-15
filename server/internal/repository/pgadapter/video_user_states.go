package pgadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

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

func (a *PGAdapter) UpdateVideoWatchProgress(ctx context.Context, userID string, videoID int64, positionSeconds float64, completed bool, at time.Time) (*repository.VideoUserState, error) {
	row, err := a.queries.UpdateVideoWatchProgress(ctx, pggen.UpdateVideoWatchProgressParams{
		UserID:          userID,
		ID:              videoID,
		PositionSeconds: positionSeconds,
		ProgressAtMs:    at.UnixMilli(),
		Completed:       completed,
		StartedSeconds:  repository.WatchStartedSeconds,
		StartedFraction: repository.WatchStartedFraction,
	})
	if err != nil {
		return nil, fmt.Errorf("pg update video watch progress: %w", mapErr(err))
	}
	return pgVideoUserStateToDomain(row), nil
}

func (a *PGAdapter) ListContinueWatchingVideos(ctx context.Context, userID string, limit int) ([]repository.Video, error) {
	page, err := a.ListVideosPage(ctx, repository.ListVideosOpts{
		UserID: userID, Limit: limit, ContinueWatchingOnly: true, Sort: "last_watched", Order: "desc",
	}, nil)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}
