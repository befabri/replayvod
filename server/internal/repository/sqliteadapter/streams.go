package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) UpsertStream(ctx context.Context, s *repository.StreamInput) (*repository.Stream, error) {
	row, err := a.queries.UpsertStream(ctx, sqlitegen.UpsertStreamParams{
		ID:            s.ID,
		BroadcasterID: s.BroadcasterID,
		Type:          s.Type,
		Language:      s.Language,
		ThumbnailUrl:  toNullString(s.ThumbnailURL),
		ViewerCount:   s.ViewerCount,
		IsMature:      boolToNullInt64(s.IsMature),
		StartedAt:     sqliteTime(s.StartedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert stream %s: %w", s.ID, err)
	}
	return sqliteStreamToDomain(row), nil
}

func (a *SQLiteAdapter) ListLatestLivePerChannel(ctx context.Context, limit int) ([]repository.LatestLiveStream, error) {
	rows, err := a.queries.ListLatestLivePerChannel(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("sqlite list latest live per channel: %w", err)
	}
	out := make([]repository.LatestLiveStream, len(rows))
	for i, r := range rows {
		out[i] = repository.LatestLiveStream{
			Stream: repository.Stream{
				ID:            r.ID,
				BroadcasterID: r.BroadcasterID,
				Type:          r.Type,
				Language:      r.Language,
				ThumbnailURL:  fromNullString(r.ThumbnailUrl),
				ViewerCount:   r.ViewerCount,
				IsMature:      nullInt64ToBool(r.IsMature),
				StartedAt:     r.StartedAt.Time,
				EndedAt:       timePtrFromSQLite(r.EndedAt),
				CreatedAt:     r.CreatedAt.Time,
			},
			BroadcasterLogin: r.BroadcasterLogin,
			BroadcasterName:  r.BroadcasterName,
			ProfileImageURL:  fromNullString(r.ProfileImageUrl),
		}
	}
	return out, nil
}

func boolToNullInt64(b *bool) sql.NullInt64 {
	if b == nil {
		return sql.NullInt64{}
	}
	v := int64(0)
	if *b {
		v = 1
	}
	return sql.NullInt64{Int64: v, Valid: true}
}

func nullInt64ToBool(n sql.NullInt64) *bool {
	if !n.Valid {
		return nil
	}
	b := n.Int64 != 0
	return &b
}
