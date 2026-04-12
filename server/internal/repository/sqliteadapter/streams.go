package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) GetStream(ctx context.Context, id string) (*repository.Stream, error) {
	row, err := a.queries.GetStream(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteStreamToDomain(row), nil
}

func (a *SQLiteAdapter) UpsertStream(ctx context.Context, s *repository.StreamInput) (*repository.Stream, error) {
	row, err := a.queries.UpsertStream(ctx, sqlitegen.UpsertStreamParams{
		ID:            s.ID,
		BroadcasterID: s.BroadcasterID,
		Type:          s.Type,
		Language:      s.Language,
		ThumbnailUrl:  toNullString(s.ThumbnailURL),
		ViewerCount:   s.ViewerCount,
		IsMature:      boolToNullInt64(s.IsMature),
		StartedAt:     formatTime(s.StartedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert stream %s: %w", s.ID, err)
	}
	return sqliteStreamToDomain(row), nil
}

func (a *SQLiteAdapter) EndStream(ctx context.Context, id string, endedAt time.Time) error {
	return a.queries.EndStream(ctx, sqlitegen.EndStreamParams{
		ID:      id,
		EndedAt: sql.NullString{String: formatTime(endedAt), Valid: true},
	})
}

func (a *SQLiteAdapter) UpdateStreamViewers(ctx context.Context, id string, viewerCount int64) error {
	return a.queries.UpdateStreamViewers(ctx, sqlitegen.UpdateStreamViewersParams{
		ID:          id,
		ViewerCount: viewerCount,
	})
}

func (a *SQLiteAdapter) ListActiveStreams(ctx context.Context) ([]repository.Stream, error) {
	rows, err := a.queries.ListActiveStreams(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite list active streams: %w", err)
	}
	return sqliteStreamsToDomain(rows), nil
}

func (a *SQLiteAdapter) ListStreamsByBroadcaster(ctx context.Context, broadcasterID string, limit, offset int) ([]repository.Stream, error) {
	rows, err := a.queries.ListStreamsByBroadcaster(ctx, sqlitegen.ListStreamsByBroadcasterParams{
		BroadcasterID: broadcasterID,
		Limit:         int64(limit),
		Offset:        int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list streams by broadcaster: %w", err)
	}
	return sqliteStreamsToDomain(rows), nil
}

func (a *SQLiteAdapter) GetLastLiveStream(ctx context.Context, broadcasterID string) (*repository.Stream, error) {
	row, err := a.queries.GetLastLiveStream(ctx, broadcasterID)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteStreamToDomain(row), nil
}

func sqliteStreamToDomain(s sqlitegen.Stream) *repository.Stream {
	return &repository.Stream{
		ID:            s.ID,
		BroadcasterID: s.BroadcasterID,
		Type:          s.Type,
		Language:      s.Language,
		ThumbnailURL:  fromNullString(s.ThumbnailUrl),
		ViewerCount:   s.ViewerCount,
		IsMature:      nullInt64ToBool(s.IsMature),
		StartedAt:     parseTime(s.StartedAt),
		EndedAt:       parseNullTime(s.EndedAt),
		CreatedAt:     parseTime(s.CreatedAt),
	}
}

func sqliteStreamsToDomain(rows []sqlitegen.Stream) []repository.Stream {
	out := make([]repository.Stream, len(rows))
	for i, r := range rows {
		out[i] = *sqliteStreamToDomain(r)
	}
	return out
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

func parseNullTime(s sql.NullString) *time.Time {
	if !s.Valid {
		return nil
	}
	t := parseTime(s.String)
	return &t
}
