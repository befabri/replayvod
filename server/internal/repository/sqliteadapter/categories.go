package sqliteadapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitetype"
)

// Categories

func (a *SQLiteAdapter) GetCategory(ctx context.Context, id string) (*repository.Category, error) {
	row, err := a.queries.GetCategory(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteCategoryToDomain(row), nil
}

func (a *SQLiteAdapter) GetCategoryDetail(ctx context.Context, id string) (*repository.CategoryDetail, error) {
	row, err := a.queries.GetCategoryDetail(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return &repository.CategoryDetail{
		Category: repository.Category{
			ID:                    row.ID,
			Name:                  row.Name,
			BoxArtURL:             fromNullString(row.BoxArtUrl),
			IGDBID:                fromNullString(row.IgdbID),
			Description:           fromNullString(row.Description),
			GameMetadataCheckedAt: timePtrFromSQLite(row.GameMetadataCheckedAt),
			DescriptionCheckedAt:  timePtrFromSQLite(row.DescriptionCheckedAt),
			CreatedAt:             row.CreatedAt.Time,
			UpdatedAt:             row.UpdatedAt.Time,
		},
		VideoCount: row.VideoCount,
		TotalSize:  row.TotalSize,
	}, nil
}

func (a *SQLiteAdapter) GetCategoryByName(ctx context.Context, name string) (*repository.Category, error) {
	row, err := a.queries.GetCategoryByName(ctx, name)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteCategoryToDomain(row), nil
}

func (a *SQLiteAdapter) UpsertCategory(ctx context.Context, c *repository.Category) (*repository.Category, error) {
	row, err := a.queries.UpsertCategory(ctx, sqlitegen.UpsertCategoryParams{
		ID:          c.ID,
		Name:        c.Name,
		BoxArtUrl:   toNullString(c.BoxArtURL),
		IgdbID:      toNullString(c.IGDBID),
		Description: toNullString(c.Description),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert category %s: %w", c.ID, err)
	}
	return sqliteCategoryToDomain(row), nil
}

const upsertCategoriesPrefix = `INSERT INTO categories (id, name, box_art_url, igdb_id, description)
VALUES `

const upsertCategoriesSuffix = `
ON CONFLICT (id) DO UPDATE SET
    name = excluded.name,
    box_art_url = ifnull(nullif(excluded.box_art_url, ''), categories.box_art_url),
    igdb_id = ifnull(nullif(excluded.igdb_id, ''), categories.igdb_id),
    description = CASE
        WHEN nullif(excluded.description, '') IS NOT NULL THEN excluded.description
        WHEN nullif(excluded.igdb_id, '') IS NOT NULL
             AND ifnull(categories.igdb_id, '') <> nullif(excluded.igdb_id, '')
        THEN NULL
        ELSE categories.description
    END,
    description_checked_at = CASE
        WHEN nullif(excluded.igdb_id, '') IS NOT NULL
             AND ifnull(categories.igdb_id, '') <> nullif(excluded.igdb_id, '')
        THEN NULL
        ELSE categories.description_checked_at
    END,
    updated_at = datetime('now')
RETURNING id, name, box_art_url, igdb_id, description, game_metadata_checked_at, description_checked_at, created_at, updated_at`

func (a *SQLiteAdapter) UpsertCategories(ctx context.Context, categories []repository.Category) ([]repository.Category, error) {
	categories = repository.UniqueCategoriesByID(categories)
	if len(categories) == 0 {
		return []repository.Category{}, nil
	}

	var b strings.Builder
	b.WriteString(upsertCategoriesPrefix)
	args := make([]any, 0, len(categories)*5)
	for i, c := range categories {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("(?, ?, ?, ?, ?)")
		args = append(args, c.ID, c.Name, toNullString(c.BoxArtURL), toNullString(c.IGDBID), toNullString(c.Description))
	}
	b.WriteString(upsertCategoriesSuffix)

	rows, err := a.db.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert categories batch: %w", err)
	}
	defer rows.Close()

	out := []repository.Category{}
	for rows.Next() {
		var row sqlitegen.Category
		if err := rows.Scan(
			&row.ID,
			&row.Name,
			&row.BoxArtUrl,
			&row.IgdbID,
			&row.Description,
			&row.GameMetadataCheckedAt,
			&row.DescriptionCheckedAt,
			&row.CreatedAt,
			&row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite upsert categories batch scan: %w", err)
		}
		out = append(out, *sqliteCategoryToDomain(row))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite upsert categories batch: %w", err)
	}

	ids := make([]string, len(categories))
	for i, c := range categories {
		ids[i] = c.ID
	}
	return repository.OrderCategoriesByIDs(out, ids), nil
}

func (a *SQLiteAdapter) ListCategories(ctx context.Context) ([]repository.Category, error) {
	rows, err := a.queries.ListCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite list categories: %w", err)
	}
	cats := make([]repository.Category, len(rows))
	for i, row := range rows {
		cats[i] = *sqliteCategoryToDomain(row)
	}
	return cats, nil
}

func (a *SQLiteAdapter) ListCategoriesWithVideos(ctx context.Context) ([]repository.Category, error) {
	rows, err := a.queries.ListCategoriesWithVideos(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite list categories with videos: %w", err)
	}
	cats := make([]repository.Category, len(rows))
	for i, row := range rows {
		cats[i] = *sqliteCategoryToDomain(row)
	}
	return cats, nil
}

func (a *SQLiteAdapter) ListCategoriesWithVideosPage(ctx context.Context, limit int, sort string, cursor *repository.CategoryPageCursor) (*repository.CategoryPage, error) {
	sort = repository.NormalizeCategoryPageSort(sort)
	rowLimit := int64(limit + 1)

	switch sort {
	case "latest_video_desc":
		rows, err := a.queries.ListCategoriesWithVideosPageLatestDesc(ctx, sqlitegen.ListCategoriesWithVideosPageLatestDescParams{
			CursorLatestVideoAt: sqliteCategoryCursorLatestVideoAt(cursor),
			CursorName:          sqliteCategoryCursorName(cursor),
			CursorID:            sqliteCategoryCursorID(cursor),
			RowLimit:            rowLimit,
		})
		if err != nil {
			return nil, fmt.Errorf("sqlite list category page by latest video: %w", err)
		}
		items := make([]repository.CategoryPageItem, 0, len(rows))
		for _, row := range rows {
			item, err := sqliteLatestCategoryPageItem(row)
			if err != nil {
				return nil, fmt.Errorf("sqlite list category page by latest video: %w", err)
			}
			items = append(items, item)
		}
		return repository.ToCategoryPage(items, limit, sort), nil
	case "video_count_desc":
		rows, err := a.queries.ListCategoriesWithVideosPageVideoCountDesc(ctx, sqlitegen.ListCategoriesWithVideosPageVideoCountDescParams{
			CursorVideoCount: sqliteCategoryCursorVideoCount(cursor),
			CursorName:       sqliteCategoryCursorName(cursor),
			CursorID:         sqliteCategoryCursorID(cursor),
			RowLimit:         rowLimit,
		})
		if err != nil {
			return nil, fmt.Errorf("sqlite list category page by video count: %w", err)
		}
		items := make([]repository.CategoryPageItem, 0, len(rows))
		for _, row := range rows {
			item, err := sqliteCountCategoryPageItem(row)
			if err != nil {
				return nil, fmt.Errorf("sqlite list category page by video count: %w", err)
			}
			items = append(items, item)
		}
		return repository.ToCategoryPage(items, limit, sort), nil
	default:
		rows, err := a.queries.ListCategoriesWithVideosPageNameAsc(ctx, sqlitegen.ListCategoriesWithVideosPageNameAscParams{
			CursorName: sqliteCategoryCursorName(cursor),
			CursorID:   sqliteCategoryCursorID(cursor),
			RowLimit:   rowLimit,
		})
		if err != nil {
			return nil, fmt.Errorf("sqlite list category page by name: %w", err)
		}
		items := make([]repository.CategoryPageItem, len(rows))
		for i, row := range rows {
			items[i] = repository.CategoryPageItem{Category: *sqliteCategoryToDomain(row)}
		}
		return repository.ToCategoryPage(items, limit, sort), nil
	}
}

func (a *SQLiteAdapter) ListCategoriesByIDs(ctx context.Context, ids []string) ([]repository.Category, error) {
	if len(ids) == 0 {
		return []repository.Category{}, nil
	}
	rows, err := a.queries.ListCategoriesByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("sqlite list categories by IDs: %w", err)
	}
	cats := make([]repository.Category, len(rows))
	for i, row := range rows {
		cats[i] = *sqliteCategoryToDomain(row)
	}
	return repository.OrderCategoriesByIDs(cats, ids), nil
}

func (a *SQLiteAdapter) SearchCategories(ctx context.Context, query string, limit int) ([]repository.Category, error) {
	rows, err := a.queries.SearchCategories(ctx, sqlitegen.SearchCategoriesParams{
		Query:    query,
		RowLimit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite search categories: %w", err)
	}
	out := make([]repository.Category, len(rows))
	for i, row := range rows {
		out[i] = *sqliteCategoryToDomain(row)
	}
	return out, nil
}

func (a *SQLiteAdapter) SearchCategoriesWithVideos(ctx context.Context, query string, limit int) ([]repository.Category, error) {
	rows, err := a.queries.SearchCategoriesWithVideos(ctx, sqlitegen.SearchCategoriesWithVideosParams{
		Query:    query,
		RowLimit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite search categories with videos: %w", err)
	}
	out := make([]repository.Category, len(rows))
	for i, row := range rows {
		out[i] = *sqliteCategoryToDomain(row)
	}
	return out, nil
}

func (a *SQLiteAdapter) ListCategoriesMissingGameMetadata(ctx context.Context, checkedBefore time.Time) ([]repository.Category, error) {
	rows, err := a.queries.ListCategoriesMissingGameMetadata(ctx, sqliteTimePtr(&checkedBefore))
	if err != nil {
		return nil, fmt.Errorf("sqlite list categories missing game metadata: %w", err)
	}
	cats := make([]repository.Category, len(rows))
	for i, row := range rows {
		cats[i] = *sqliteCategoryToDomain(row)
	}
	return cats, nil
}

func (a *SQLiteAdapter) UpdateCategoryGameMetadata(ctx context.Context, id, boxArtURL, igdbID string) error {
	if err := a.queries.UpdateCategoryGameMetadata(ctx, sqlitegen.UpdateCategoryGameMetadataParams{
		ID:       id,
		NULLIF:   boxArtURL,
		NULLIF_2: igdbID,
	}); err != nil {
		return fmt.Errorf("sqlite update category game metadata %s: %w", id, err)
	}
	return nil
}

func (a *SQLiteAdapter) MarkCategoryGameMetadataChecked(ctx context.Context, id string) error {
	if err := a.queries.MarkCategoryGameMetadataChecked(ctx, id); err != nil {
		return fmt.Errorf("sqlite mark category game metadata checked %s: %w", id, err)
	}
	return nil
}

func (a *SQLiteAdapter) ListCategoriesMissingDescription(ctx context.Context, checkedBefore time.Time) ([]repository.Category, error) {
	rows, err := a.queries.ListCategoriesMissingDescription(ctx, sqliteTimePtr(&checkedBefore))
	if err != nil {
		return nil, fmt.Errorf("sqlite list categories missing description: %w", err)
	}
	cats := make([]repository.Category, len(rows))
	for i, row := range rows {
		cats[i] = *sqliteCategoryToDomain(row)
	}
	return cats, nil
}

func (a *SQLiteAdapter) UpdateCategoryDescription(ctx context.Context, id, description string) error {
	if err := a.queries.UpdateCategoryDescription(ctx, sqlitegen.UpdateCategoryDescriptionParams{
		ID:          id,
		Description: toNullString(&description),
	}); err != nil {
		return fmt.Errorf("sqlite update category description %s: %w", id, err)
	}
	return nil
}

func (a *SQLiteAdapter) MarkCategoryDescriptionChecked(ctx context.Context, id string) error {
	if err := a.queries.MarkCategoryDescriptionChecked(ctx, id); err != nil {
		return fmt.Errorf("sqlite mark category description checked %s: %w", id, err)
	}
	return nil
}

func (a *SQLiteAdapter) GetCategorySearchCache(ctx context.Context, normalizedQuery string) (*repository.CategorySearchCache, error) {
	row, err := a.queries.GetCategorySearchCache(ctx, normalizedQuery)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteCategorySearchCacheToDomain(row)
}

func (a *SQLiteAdapter) UpsertCategorySearchCache(ctx context.Context, input repository.CategorySearchCacheInput) (*repository.CategorySearchCache, error) {
	categoryIDs, err := json.Marshal(input.CategoryIDs)
	if err != nil {
		return nil, fmt.Errorf("sqlite marshal category search cache IDs: %w", err)
	}
	row, err := a.queries.UpsertCategorySearchCache(ctx, sqlitegen.UpsertCategorySearchCacheParams{
		NormalizedQuery: input.NormalizedQuery,
		CategoryIds:     string(categoryIDs),
		ExpiresAt:       sqliteTime(input.ExpiresAt),
		LastAccessedAt:  sqliteTime(input.LastAccessedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert category search cache %q: %w", input.NormalizedQuery, err)
	}
	return sqliteCategorySearchCacheToDomain(row)
}

func (a *SQLiteAdapter) TouchCategorySearchCache(ctx context.Context, normalizedQuery string, at time.Time) error {
	if err := a.queries.TouchCategorySearchCache(ctx, sqlitegen.TouchCategorySearchCacheParams{
		LastAccessedAt:  sqliteTime(at),
		NormalizedQuery: normalizedQuery,
	}); err != nil {
		return fmt.Errorf("sqlite touch category search cache %q: %w", normalizedQuery, err)
	}
	return nil
}

func (a *SQLiteAdapter) DeleteExpiredCategorySearchCache(ctx context.Context, before time.Time) error {
	if err := a.queries.DeleteExpiredCategorySearchCache(ctx, sqliteTime(before)); err != nil {
		return fmt.Errorf("sqlite delete expired category search cache: %w", err)
	}
	return nil
}

func (a *SQLiteAdapter) PruneCategorySearchCache(ctx context.Context, maxRows int) error {
	if maxRows < 0 {
		maxRows = 0
	}
	if err := a.queries.PruneCategorySearchCache(ctx, int64(maxRows)); err != nil {
		return fmt.Errorf("sqlite prune category search cache: %w", err)
	}
	return nil
}

func sqliteCategoryCursorName(cursor *repository.CategoryPageCursor) sql.NullString {
	if cursor == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: cursor.Name, Valid: true}
}

func sqliteCategoryCursorID(cursor *repository.CategoryPageCursor) string {
	if cursor == nil {
		return ""
	}
	return cursor.ID
}

func sqliteCategoryCursorLatestVideoAt(cursor *repository.CategoryPageCursor) sql.NullString {
	if cursor == nil || cursor.LatestVideoAt == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: sqlitetype.Format(*cursor.LatestVideoAt), Valid: true}
}

func sqliteCategoryCursorVideoCount(cursor *repository.CategoryPageCursor) int64 {
	if cursor == nil {
		return 0
	}
	return cursor.VideoCount
}

func sqliteLatestCategoryPageItem(row sqlitegen.ListCategoriesWithVideosPageLatestDescRow) (repository.CategoryPageItem, error) {
	latest, err := sqliteCategoryPageTime(row.LatestVideoAt)
	if err != nil {
		return repository.CategoryPageItem{}, err
	}
	return repository.CategoryPageItem{
		Category: repository.Category{
			ID:                    row.ID,
			Name:                  row.Name,
			BoxArtURL:             fromNullString(row.BoxArtUrl),
			IGDBID:                fromNullString(row.IgdbID),
			Description:           fromNullString(row.Description),
			GameMetadataCheckedAt: timePtrFromSQLite(row.GameMetadataCheckedAt),
			DescriptionCheckedAt:  timePtrFromSQLite(row.DescriptionCheckedAt),
			CreatedAt:             row.CreatedAt.Time,
			UpdatedAt:             row.UpdatedAt.Time,
		},
		LatestVideoAt: latest,
		VideoCount:    row.VideoCount,
	}, nil
}

func sqliteCountCategoryPageItem(row sqlitegen.ListCategoriesWithVideosPageVideoCountDescRow) (repository.CategoryPageItem, error) {
	latest, err := sqliteCategoryPageTime(row.LatestVideoAt)
	if err != nil {
		return repository.CategoryPageItem{}, err
	}
	return repository.CategoryPageItem{
		Category: repository.Category{
			ID:                    row.ID,
			Name:                  row.Name,
			BoxArtURL:             fromNullString(row.BoxArtUrl),
			IGDBID:                fromNullString(row.IgdbID),
			Description:           fromNullString(row.Description),
			GameMetadataCheckedAt: timePtrFromSQLite(row.GameMetadataCheckedAt),
			DescriptionCheckedAt:  timePtrFromSQLite(row.DescriptionCheckedAt),
			CreatedAt:             row.CreatedAt.Time,
			UpdatedAt:             row.UpdatedAt.Time,
		},
		LatestVideoAt: latest,
		VideoCount:    row.VideoCount,
	}, nil
}

func sqliteCategoryPageTime(v any) (time.Time, error) {
	switch x := v.(type) {
	case time.Time:
		return x.UTC(), nil
	case string:
		return sqlitetype.Parse(x)
	case []byte:
		return sqlitetype.Parse(string(x))
	default:
		return time.Time{}, fmt.Errorf("sqlite category page timestamp: cannot scan %T", v)
	}
}

// Tags

func (a *SQLiteAdapter) GetTag(ctx context.Context, id int64) (*repository.Tag, error) {
	row, err := a.queries.GetTag(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteTagToDomain(row), nil
}

func (a *SQLiteAdapter) GetTagByName(ctx context.Context, name string) (*repository.Tag, error) {
	row, err := a.queries.GetTagByName(ctx, name)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteTagToDomain(row), nil
}

func (a *SQLiteAdapter) UpsertTag(ctx context.Context, name string) (*repository.Tag, error) {
	row, err := a.queries.UpsertTag(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert tag %s: %w", name, err)
	}
	return sqliteTagToDomain(row), nil
}

func (a *SQLiteAdapter) ListTags(ctx context.Context) ([]repository.Tag, error) {
	rows, err := a.queries.ListTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite list tags: %w", err)
	}
	tags := make([]repository.Tag, len(rows))
	for i, row := range rows {
		tags[i] = *sqliteTagToDomain(row)
	}
	return tags, nil
}

func sqliteCategoryToDomain(c sqlitegen.Category) *repository.Category {
	return &repository.Category{
		ID:                    c.ID,
		Name:                  c.Name,
		BoxArtURL:             fromNullString(c.BoxArtUrl),
		IGDBID:                fromNullString(c.IgdbID),
		Description:           fromNullString(c.Description),
		GameMetadataCheckedAt: timePtrFromSQLite(c.GameMetadataCheckedAt),
		DescriptionCheckedAt:  timePtrFromSQLite(c.DescriptionCheckedAt),
		CreatedAt:             c.CreatedAt.Time,
		UpdatedAt:             c.UpdatedAt.Time,
	}
}

func sqliteCategorySearchCacheToDomain(c sqlitegen.CategorySearchCache) (*repository.CategorySearchCache, error) {
	var categoryIDs []string
	if err := json.Unmarshal([]byte(c.CategoryIds), &categoryIDs); err != nil {
		return nil, fmt.Errorf("sqlite decode category search cache %q IDs: %w", c.NormalizedQuery, err)
	}
	return &repository.CategorySearchCache{
		NormalizedQuery: c.NormalizedQuery,
		CategoryIDs:     categoryIDs,
		ExpiresAt:       c.ExpiresAt.Time,
		LastAccessedAt:  c.LastAccessedAt.Time,
		CreatedAt:       c.CreatedAt.Time,
		UpdatedAt:       c.UpdatedAt.Time,
	}, nil
}
