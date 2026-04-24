package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

const listChannelsPageAscSQL = `SELECT
    c.broadcaster_id, c.broadcaster_login, c.broadcaster_name, c.broadcaster_language,
    c.profile_image_url, c.offline_image_url, c.description, c.broadcaster_type,
    c.view_count, c.created_at, c.updated_at
FROM channels c
WHERE (
    ?1 = 0
    OR EXISTS (
        SELECT 1 FROM streams s
        WHERE s.broadcaster_id = c.broadcaster_id AND s.ended_at IS NULL
    )
)
  AND (
    ?2 IS NULL
    OR lower(c.broadcaster_name) > lower(?2)
    OR (lower(c.broadcaster_name) = lower(?2) AND c.broadcaster_id > ?3)
  )
ORDER BY lower(c.broadcaster_name) ASC, c.broadcaster_id ASC
LIMIT ?4`

const listChannelsPageDescSQL = `SELECT
    c.broadcaster_id, c.broadcaster_login, c.broadcaster_name, c.broadcaster_language,
    c.profile_image_url, c.offline_image_url, c.description, c.broadcaster_type,
    c.view_count, c.created_at, c.updated_at
FROM channels c
WHERE (
    ?1 = 0
    OR EXISTS (
        SELECT 1 FROM streams s
        WHERE s.broadcaster_id = c.broadcaster_id AND s.ended_at IS NULL
    )
)
  AND (
    ?2 IS NULL
    OR lower(c.broadcaster_name) < lower(?2)
    OR (lower(c.broadcaster_name) = lower(?2) AND c.broadcaster_id < ?3)
  )
ORDER BY lower(c.broadcaster_name) DESC, c.broadcaster_id DESC
LIMIT ?4`

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

func (a *SQLiteAdapter) ListChannelsPage(ctx context.Context, limit int, sort string, liveOnly bool, cursor *repository.ChannelPageCursor) (*repository.ChannelPage, error) {
	query := listChannelsPageAscSQL
	if sort == "name_desc" {
		query = listChannelsPageDescSQL
	}
	rows, err := a.db.QueryContext(ctx, query, boolToInt(liveOnly), sqliteChannelCursorName(cursor), sqliteChannelCursorID(cursor), int64(limit+1))
	if err != nil {
		return nil, fmt.Errorf("sqlite list channels page: %w", err)
	}
	items, err := scanSQLiteChannels(rows)
	if err != nil {
		return nil, fmt.Errorf("sqlite list channels page: %w", err)
	}
	return toChannelPage(items, limit), nil
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

func scanSQLiteChannels(rows *sql.Rows) ([]repository.Channel, error) {
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
			return nil, err
		}
		out = append(out, *sqliteChannelToDomain(row))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func sqliteChannelCursorName(cursor *repository.ChannelPageCursor) any {
	if cursor == nil {
		return nil
	}
	return cursor.BroadcasterName
}

func sqliteChannelCursorID(cursor *repository.ChannelPageCursor) string {
	if cursor == nil {
		return ""
	}
	return cursor.BroadcasterID
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func toChannelPage(items []repository.Channel, limit int) *repository.ChannelPage {
	if limit <= 0 {
		return &repository.ChannelPage{Items: []repository.Channel{}}
	}
	page := &repository.ChannelPage{Items: items}
	if len(items) <= limit {
		return page
	}
	page.Items = items[:limit]
	next := page.Items[len(page.Items)-1]
	page.NextCursor = &repository.ChannelPageCursor{
		BroadcasterName: next.BroadcasterName,
		BroadcasterID:   next.BroadcasterID,
	}
	return page
}
