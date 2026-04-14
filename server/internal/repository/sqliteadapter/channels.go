package sqliteadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

// Channels

func (a *SQLiteAdapter) GetChannel(ctx context.Context, broadcasterID string) (*repository.Channel, error) {
	row, err := a.queries.GetChannel(ctx, broadcasterID)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteChannelToDomain(row), nil
}

func (a *SQLiteAdapter) GetChannelByLogin(ctx context.Context, login string) (*repository.Channel, error) {
	row, err := a.queries.GetChannelByLogin(ctx, login)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteChannelToDomain(row), nil
}

func (a *SQLiteAdapter) UpsertChannel(ctx context.Context, c *repository.Channel) (*repository.Channel, error) {
	row, err := a.queries.UpsertChannel(ctx, sqlitegen.UpsertChannelParams{
		BroadcasterID:       c.BroadcasterID,
		BroadcasterLogin:    c.BroadcasterLogin,
		BroadcasterName:     c.BroadcasterName,
		BroadcasterLanguage: toNullString(c.BroadcasterLanguage),
		ProfileImageUrl:     toNullString(c.ProfileImageURL),
		OfflineImageUrl:     toNullString(c.OfflineImageURL),
		Description:         toNullString(c.Description),
		BroadcasterType:     toNullString(c.BroadcasterType),
		ViewCount:           c.ViewCount,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert channel %s: %w", c.BroadcasterID, err)
	}
	return sqliteChannelToDomain(row), nil
}

func (a *SQLiteAdapter) ListChannels(ctx context.Context) ([]repository.Channel, error) {
	rows, err := a.queries.ListChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite list channels: %w", err)
	}
	channels := make([]repository.Channel, len(rows))
	for i, row := range rows {
		channels[i] = *sqliteChannelToDomain(row)
	}
	return channels, nil
}

func (a *SQLiteAdapter) ListChannelsByIDs(ctx context.Context, ids []string) ([]repository.Channel, error) {
	if len(ids) == 0 {
		return []repository.Channel{}, nil
	}
	rows, err := a.queries.ListChannelsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("sqlite list channels by ids: %w", err)
	}
	channels := make([]repository.Channel, len(rows))
	for i, row := range rows {
		channels[i] = *sqliteChannelToDomain(row)
	}
	return channels, nil
}

// searchChannelsSQL mirrors queries/postgres/channels.sql SearchChannels.
// Hand-rolled because sqlc's SQLite engine can't type-infer a ?N param
// whose only usages are inside a CASE expression (see the NOTE in
// queries/sqlite/channels.sql).
const searchChannelsSQL = `SELECT
    broadcaster_id, broadcaster_login, broadcaster_name, broadcaster_language,
    profile_image_url, offline_image_url, description, broadcaster_type,
    view_count, created_at, updated_at
FROM channels
WHERE ?1 = ''
   OR lower(broadcaster_login) LIKE '%' || lower(?1) || '%'
   OR lower(broadcaster_name)  LIKE '%' || lower(?1) || '%'
ORDER BY
    CASE
        WHEN ?1 = '' THEN 3
        WHEN lower(broadcaster_login) = lower(?1) THEN 0
        WHEN lower(broadcaster_login) LIKE lower(?1) || '%' THEN 1
        WHEN lower(broadcaster_name)  LIKE lower(?1) || '%' THEN 1
        ELSE 2
    END,
    broadcaster_login
LIMIT ?2`

func (a *SQLiteAdapter) SearchChannels(ctx context.Context, query string, limit int) ([]repository.Channel, error) {
	rows, err := a.db.QueryContext(ctx, searchChannelsSQL, query, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("sqlite search channels: %w", err)
	}
	defer rows.Close()
	out := []repository.Channel{}
	for rows.Next() {
		var row sqlitegen.Channel
		if err := rows.Scan(
			&row.BroadcasterID,
			&row.BroadcasterLogin,
			&row.BroadcasterName,
			&row.BroadcasterLanguage,
			&row.ProfileImageUrl,
			&row.OfflineImageUrl,
			&row.Description,
			&row.BroadcasterType,
			&row.ViewCount,
			&row.CreatedAt,
			&row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite search channels scan: %w", err)
		}
		out = append(out, *sqliteChannelToDomain(row))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite search channels: %w", err)
	}
	return out, nil
}

func (a *SQLiteAdapter) DeleteChannel(ctx context.Context, broadcasterID string) error {
	return a.queries.DeleteChannel(ctx, broadcasterID)
}

// User follows

func (a *SQLiteAdapter) UpsertUserFollow(ctx context.Context, f *repository.UserFollow) error {
	followed := int64(0)
	if f.Followed {
		followed = 1
	}
	return a.queries.UpsertUserFollow(ctx, sqlitegen.UpsertUserFollowParams{
		UserID:        f.UserID,
		BroadcasterID: f.BroadcasterID,
		FollowedAt:    formatTime(f.FollowedAt),
		Followed:      followed,
	})
}

func (a *SQLiteAdapter) ListUserFollows(ctx context.Context, userID string) ([]repository.Channel, error) {
	rows, err := a.queries.ListUserFollows(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("sqlite list user follows: %w", err)
	}
	channels := make([]repository.Channel, len(rows))
	for i, row := range rows {
		channels[i] = *sqliteChannelToDomain(row)
	}
	return channels, nil
}

func (a *SQLiteAdapter) UnfollowChannel(ctx context.Context, userID, broadcasterID string) error {
	return a.queries.UnfollowChannel(ctx, sqlitegen.UnfollowChannelParams{
		UserID:        userID,
		BroadcasterID: broadcasterID,
	})
}

func sqliteChannelToDomain(c sqlitegen.Channel) *repository.Channel {
	return &repository.Channel{
		BroadcasterID:       c.BroadcasterID,
		BroadcasterLogin:    c.BroadcasterLogin,
		BroadcasterName:     c.BroadcasterName,
		BroadcasterLanguage: fromNullString(c.BroadcasterLanguage),
		ProfileImageURL:     fromNullString(c.ProfileImageUrl),
		OfflineImageURL:     fromNullString(c.OfflineImageUrl),
		Description:         fromNullString(c.Description),
		BroadcasterType:     fromNullString(c.BroadcasterType),
		ViewCount:           c.ViewCount,
		CreatedAt:           parseTime(c.CreatedAt),
		UpdatedAt:           parseTime(c.UpdatedAt),
	}
}
