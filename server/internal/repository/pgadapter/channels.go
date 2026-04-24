package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
	"github.com/jackc/pgx/v5"
)

const listChannelsPageAscSQL = `SELECT
    c.broadcaster_id, c.broadcaster_login, c.broadcaster_name, c.broadcaster_language,
    c.profile_image_url, c.offline_image_url, c.description, c.broadcaster_type,
    c.view_count, c.created_at, c.updated_at
FROM channels c
WHERE (
    NOT $1
    OR EXISTS (
        SELECT 1 FROM streams s
        WHERE s.broadcaster_id = c.broadcaster_id AND s.ended_at IS NULL
    )
)
  AND (
    $2::text IS NULL
    OR lower(c.broadcaster_name) > lower($2::text)
    OR (lower(c.broadcaster_name) = lower($2::text) AND c.broadcaster_id > $3)
  )
ORDER BY lower(c.broadcaster_name) ASC, c.broadcaster_id ASC
LIMIT $4`

const listChannelsPageDescSQL = `SELECT
    c.broadcaster_id, c.broadcaster_login, c.broadcaster_name, c.broadcaster_language,
    c.profile_image_url, c.offline_image_url, c.description, c.broadcaster_type,
    c.view_count, c.created_at, c.updated_at
FROM channels c
WHERE (
    NOT $1
    OR EXISTS (
        SELECT 1 FROM streams s
        WHERE s.broadcaster_id = c.broadcaster_id AND s.ended_at IS NULL
    )
)
  AND (
    $2::text IS NULL
    OR lower(c.broadcaster_name) < lower($2::text)
    OR (lower(c.broadcaster_name) = lower($2::text) AND c.broadcaster_id < $3)
  )
ORDER BY lower(c.broadcaster_name) DESC, c.broadcaster_id DESC
LIMIT $4`

func (a *PGAdapter) GetChannel(ctx context.Context, broadcasterID string) (*repository.Channel, error) {
	row, err := a.queries.GetChannel(ctx, broadcasterID)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgChannelToDomain(row), nil
}

func (a *PGAdapter) GetChannelByLogin(ctx context.Context, login string) (*repository.Channel, error) {
	row, err := a.queries.GetChannelByLogin(ctx, login)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgChannelToDomain(row), nil
}

func (a *PGAdapter) UpsertChannel(ctx context.Context, c *repository.Channel) (*repository.Channel, error) {
	row, err := a.queries.UpsertChannel(ctx, pggen.UpsertChannelParams{
		BroadcasterID:       c.BroadcasterID,
		BroadcasterLogin:    c.BroadcasterLogin,
		BroadcasterName:     c.BroadcasterName,
		BroadcasterLanguage: c.BroadcasterLanguage,
		ProfileImageUrl:     c.ProfileImageURL,
		OfflineImageUrl:     c.OfflineImageURL,
		Description:         c.Description,
		BroadcasterType:     c.BroadcasterType,
		ViewCount:           int32(c.ViewCount),
	})
	if err != nil {
		return nil, fmt.Errorf("pg upsert channel %s: %w", c.BroadcasterID, err)
	}
	return pgChannelToDomain(row), nil
}

func (a *PGAdapter) ListChannels(ctx context.Context) ([]repository.Channel, error) {
	rows, err := a.queries.ListChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg list channels: %w", err)
	}
	channels := make([]repository.Channel, len(rows))
	for i, row := range rows {
		channels[i] = *pgChannelToDomain(row)
	}
	return channels, nil
}

func (a *PGAdapter) ListChannelsPage(ctx context.Context, limit int, sort string, liveOnly bool, cursor *repository.ChannelPageCursor) (*repository.ChannelPage, error) {
	query := listChannelsPageAscSQL
	if sort == "name_desc" {
		query = listChannelsPageDescSQL
	}
	rows, err := a.db.Query(ctx, query, liveOnly, pgChannelCursorName(cursor), pgChannelCursorID(cursor), limit+1)
	if err != nil {
		return nil, fmt.Errorf("pg list channels page: %w", err)
	}
	items, err := scanPGChannels(rows)
	if err != nil {
		return nil, fmt.Errorf("pg list channels page: %w", err)
	}
	return toChannelPage(items, limit), nil
}

func (a *PGAdapter) ListChannelsByIDs(ctx context.Context, ids []string) ([]repository.Channel, error) {
	if len(ids) == 0 {
		return []repository.Channel{}, nil
	}
	rows, err := a.queries.ListChannelsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("pg list channels by ids: %w", err)
	}
	channels := make([]repository.Channel, len(rows))
	for i, row := range rows {
		channels[i] = *pgChannelToDomain(row)
	}
	return channels, nil
}

func (a *PGAdapter) SearchChannels(ctx context.Context, query string, limit int) ([]repository.Channel, error) {
	rows, err := a.queries.SearchChannels(ctx, pggen.SearchChannelsParams{
		Query:    query,
		RowLimit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("pg search channels: %w", err)
	}
	channels := make([]repository.Channel, len(rows))
	for i, row := range rows {
		channels[i] = *pgChannelToDomain(row)
	}
	return channels, nil
}

func (a *PGAdapter) DeleteChannel(ctx context.Context, broadcasterID string) error {
	return a.queries.DeleteChannel(ctx, broadcasterID)
}

func (a *PGAdapter) UpsertUserFollow(ctx context.Context, f *repository.UserFollow) error {
	return a.queries.UpsertUserFollow(ctx, pggen.UpsertUserFollowParams{
		UserID:        f.UserID,
		BroadcasterID: f.BroadcasterID,
		FollowedAt:    f.FollowedAt,
		Followed:      f.Followed,
	})
}

func (a *PGAdapter) ListUserFollows(ctx context.Context, userID string) ([]repository.Channel, error) {
	rows, err := a.queries.ListUserFollows(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("pg list user follows: %w", err)
	}
	channels := make([]repository.Channel, len(rows))
	for i, row := range rows {
		channels[i] = *pgChannelToDomain(row)
	}
	return channels, nil
}

func (a *PGAdapter) UnfollowChannel(ctx context.Context, userID, broadcasterID string) error {
	return a.queries.UnfollowChannel(ctx, pggen.UnfollowChannelParams{
		UserID:        userID,
		BroadcasterID: broadcasterID,
	})
}

func pgChannelToDomain(c pggen.Channel) *repository.Channel {
	return &repository.Channel{
		BroadcasterID:       c.BroadcasterID,
		BroadcasterLogin:    c.BroadcasterLogin,
		BroadcasterName:     c.BroadcasterName,
		BroadcasterLanguage: c.BroadcasterLanguage,
		ProfileImageURL:     c.ProfileImageUrl,
		OfflineImageURL:     c.OfflineImageUrl,
		Description:         c.Description,
		BroadcasterType:     c.BroadcasterType,
		ViewCount:           int64(c.ViewCount),
		CreatedAt:           c.CreatedAt,
		UpdatedAt:           c.UpdatedAt,
	}
}

func scanPGChannels(rows pgx.Rows) ([]repository.Channel, error) {
	defer rows.Close()
	items := []repository.Channel{}
	for rows.Next() {
		var row pggen.Channel
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
		items = append(items, *pgChannelToDomain(row))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func pgChannelCursorName(cursor *repository.ChannelPageCursor) *string {
	if cursor == nil {
		return nil
	}
	return &cursor.BroadcasterName
}

func pgChannelCursorID(cursor *repository.ChannelPageCursor) string {
	if cursor == nil {
		return ""
	}
	return cursor.BroadcasterID
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
