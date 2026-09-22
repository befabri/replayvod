package sqliteadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
)

func (a *SQLiteAdapter) ListVideoPartsForVideos(ctx context.Context, videoIDs []int64) ([]repository.VideoPart, error) {
	if len(videoIDs) == 0 {
		return nil, nil
	}
	rows, err := a.queries.ListVideoPartsForVideos(ctx, videoIDs)
	if err != nil {
		return nil, fmt.Errorf("sqlite list video parts for videos: %w", err)
	}
	out := make([]repository.VideoPart, len(rows))
	for i, r := range rows {
		out[i] = *sqliteVideoPartToDomain(r)
	}
	return out, nil
}
