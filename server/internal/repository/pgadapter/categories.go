package pgadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

// Categories

func (a *PGAdapter) GetCategory(ctx context.Context, id string) (*repository.Category, error) {
	row, err := a.queries.GetCategory(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgCategoryToDomain(row), nil
}

func (a *PGAdapter) GetCategoryDetail(ctx context.Context, id string) (*repository.CategoryDetail, error) {
	row, err := a.queries.GetCategoryDetail(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return &repository.CategoryDetail{
		Category: repository.Category{
			ID:                    row.ID,
			Name:                  row.Name,
			BoxArtURL:             row.BoxArtUrl,
			IGDBID:                row.IgdbID,
			Description:           row.Description,
			GameMetadataCheckedAt: row.GameMetadataCheckedAt,
			DescriptionCheckedAt:  row.DescriptionCheckedAt,
			CreatedAt:             row.CreatedAt,
			UpdatedAt:             row.UpdatedAt,
		},
		VideoCount: row.VideoCount,
		TotalSize:  row.TotalSize,
	}, nil
}

func (a *PGAdapter) GetCategoryByName(ctx context.Context, name string) (*repository.Category, error) {
	row, err := a.queries.GetCategoryByName(ctx, name)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgCategoryToDomain(row), nil
}

func (a *PGAdapter) UpsertCategory(ctx context.Context, c *repository.Category) (*repository.Category, error) {
	row, err := a.queries.UpsertCategory(ctx, pggen.UpsertCategoryParams{
		ID:          c.ID,
		Name:        c.Name,
		BoxArtUrl:   c.BoxArtURL,
		IgdbID:      c.IGDBID,
		Description: c.Description,
	})
	if err != nil {
		return nil, fmt.Errorf("pg upsert category %s: %w", c.ID, err)
	}
	return pgCategoryToDomain(row), nil
}

const upsertCategoriesSQL = `WITH input AS (
    SELECT *
    FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[]) WITH ORDINALITY
        AS t(id, name, box_art_url, igdb_id, description, ord)
),
upserted AS (
    INSERT INTO categories (id, name, box_art_url, igdb_id, description)
    SELECT id, name, box_art_url, igdb_id, description
    FROM input
    ON CONFLICT (id) DO UPDATE SET
        name = EXCLUDED.name,
        box_art_url = COALESCE(NULLIF(EXCLUDED.box_art_url, ''), categories.box_art_url),
        igdb_id = COALESCE(NULLIF(EXCLUDED.igdb_id, ''), categories.igdb_id),
        description = CASE
            WHEN NULLIF(EXCLUDED.description, '') IS NOT NULL THEN EXCLUDED.description
            WHEN NULLIF(EXCLUDED.igdb_id, '') IS NOT NULL
                 AND NULLIF(EXCLUDED.igdb_id, '') IS DISTINCT FROM categories.igdb_id
            THEN NULL
            ELSE categories.description
        END,
        description_checked_at = CASE
            WHEN NULLIF(EXCLUDED.igdb_id, '') IS NOT NULL
                 AND NULLIF(EXCLUDED.igdb_id, '') IS DISTINCT FROM categories.igdb_id
            THEN NULL
            ELSE categories.description_checked_at
        END,
        updated_at = NOW()
    RETURNING id, name, box_art_url, igdb_id, description, game_metadata_checked_at, description_checked_at, created_at, updated_at
)
SELECT u.id, u.name, u.box_art_url, u.igdb_id, u.description, u.game_metadata_checked_at, u.description_checked_at, u.created_at, u.updated_at
FROM upserted u
INNER JOIN input i ON i.id = u.id
ORDER BY i.ord`

func (a *PGAdapter) UpsertCategories(ctx context.Context, categories []repository.Category) ([]repository.Category, error) {
	categories = repository.UniqueCategoriesByID(categories)
	if len(categories) == 0 {
		return []repository.Category{}, nil
	}

	ids := make([]string, len(categories))
	names := make([]string, len(categories))
	boxArtURLs := make([]*string, len(categories))
	igdbIDs := make([]*string, len(categories))
	descriptions := make([]*string, len(categories))
	for i, c := range categories {
		ids[i] = c.ID
		names[i] = c.Name
		boxArtURLs[i] = c.BoxArtURL
		igdbIDs[i] = c.IGDBID
		descriptions[i] = c.Description
	}

	rows, err := a.db.Query(ctx, upsertCategoriesSQL, ids, names, boxArtURLs, igdbIDs, descriptions)
	if err != nil {
		return nil, fmt.Errorf("pg upsert categories batch: %w", err)
	}
	defer rows.Close()

	out := []repository.Category{}
	for rows.Next() {
		var row pggen.Category
		if err := rows.Scan(&row.ID, &row.Name, &row.BoxArtUrl, &row.IgdbID, &row.Description, &row.GameMetadataCheckedAt, &row.DescriptionCheckedAt, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, fmt.Errorf("pg upsert categories batch scan: %w", err)
		}
		out = append(out, *pgCategoryToDomain(row))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pg upsert categories batch: %w", err)
	}
	return repository.OrderCategoriesByIDs(out, ids), nil
}

func (a *PGAdapter) ListCategories(ctx context.Context) ([]repository.Category, error) {
	rows, err := a.queries.ListCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg list categories: %w", err)
	}
	cats := make([]repository.Category, len(rows))
	for i, row := range rows {
		cats[i] = *pgCategoryToDomain(row)
	}
	return cats, nil
}

func (a *PGAdapter) ListCategoriesWithVideos(ctx context.Context) ([]repository.Category, error) {
	rows, err := a.queries.ListCategoriesWithVideos(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg list categories with videos: %w", err)
	}
	cats := make([]repository.Category, len(rows))
	for i, row := range rows {
		cats[i] = *pgCategoryToDomain(row)
	}
	return cats, nil
}

func (a *PGAdapter) ListCategoriesWithVideosPage(ctx context.Context, limit int, sort string, cursor *repository.CategoryPageCursor) (*repository.CategoryPage, error) {
	sort = repository.NormalizeCategoryPageSort(sort)
	rowLimit := int32(limit + 1)

	switch sort {
	case "latest_video_desc":
		rows, err := a.queries.ListCategoriesWithVideosPageLatestDesc(ctx, pggen.ListCategoriesWithVideosPageLatestDescParams{
			CursorLatestVideoAt: pgCategoryCursorLatestVideoAt(cursor),
			CursorName:          pgCategoryCursorName(cursor),
			CursorID:            pgCategoryCursorID(cursor),
			RowLimit:            rowLimit,
		})
		if err != nil {
			return nil, fmt.Errorf("pg list category page by latest video: %w", err)
		}
		items := make([]repository.CategoryPageItem, 0, len(rows))
		for _, row := range rows {
			item, err := pgLatestCategoryPageItem(row)
			if err != nil {
				return nil, fmt.Errorf("pg list category page by latest video: %w", err)
			}
			items = append(items, item)
		}
		return repository.ToCategoryPage(items, limit, sort), nil
	case "video_count_desc":
		rows, err := a.queries.ListCategoriesWithVideosPageVideoCountDesc(ctx, pggen.ListCategoriesWithVideosPageVideoCountDescParams{
			CursorName:       pgCategoryCursorName(cursor),
			CursorVideoCount: pgCategoryCursorVideoCount(cursor),
			CursorID:         pgCategoryCursorID(cursor),
			RowLimit:         rowLimit,
		})
		if err != nil {
			return nil, fmt.Errorf("pg list category page by video count: %w", err)
		}
		items := make([]repository.CategoryPageItem, 0, len(rows))
		for _, row := range rows {
			item, err := pgCountCategoryPageItem(row)
			if err != nil {
				return nil, fmt.Errorf("pg list category page by video count: %w", err)
			}
			items = append(items, item)
		}
		return repository.ToCategoryPage(items, limit, sort), nil
	default:
		rows, err := a.queries.ListCategoriesWithVideosPageNameAsc(ctx, pggen.ListCategoriesWithVideosPageNameAscParams{
			CursorName: pgCategoryCursorName(cursor),
			CursorID:   pgCategoryCursorID(cursor),
			RowLimit:   rowLimit,
		})
		if err != nil {
			return nil, fmt.Errorf("pg list category page by name: %w", err)
		}
		items := make([]repository.CategoryPageItem, len(rows))
		for i, row := range rows {
			items[i] = repository.CategoryPageItem{Category: *pgCategoryToDomain(row)}
		}
		return repository.ToCategoryPage(items, limit, sort), nil
	}
}

func (a *PGAdapter) ListCategoriesByIDs(ctx context.Context, ids []string) ([]repository.Category, error) {
	if len(ids) == 0 {
		return []repository.Category{}, nil
	}
	rows, err := a.queries.ListCategoriesByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("pg list categories by IDs: %w", err)
	}
	cats := make([]repository.Category, len(rows))
	for i, row := range rows {
		cats[i] = *pgCategoryToDomain(row)
	}
	return repository.OrderCategoriesByIDs(cats, ids), nil
}

func (a *PGAdapter) SearchCategories(ctx context.Context, query string, limit int) ([]repository.Category, error) {
	rows, err := a.queries.SearchCategories(ctx, pggen.SearchCategoriesParams{
		Query:    query,
		RowLimit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("pg search categories: %w", err)
	}
	cats := make([]repository.Category, len(rows))
	for i, row := range rows {
		cats[i] = *pgCategoryToDomain(row)
	}
	return cats, nil
}

func (a *PGAdapter) SearchCategoriesWithVideos(ctx context.Context, query string, limit int) ([]repository.Category, error) {
	rows, err := a.queries.SearchCategoriesWithVideos(ctx, pggen.SearchCategoriesWithVideosParams{
		Query:    query,
		RowLimit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("pg search categories with videos: %w", err)
	}
	cats := make([]repository.Category, len(rows))
	for i, row := range rows {
		cats[i] = *pgCategoryToDomain(row)
	}
	return cats, nil
}

func (a *PGAdapter) ListCategoriesMissingGameMetadata(ctx context.Context, checkedBefore time.Time) ([]repository.Category, error) {
	rows, err := a.queries.ListCategoriesMissingGameMetadata(ctx, &checkedBefore)
	if err != nil {
		return nil, fmt.Errorf("pg list categories missing game metadata: %w", err)
	}
	cats := make([]repository.Category, len(rows))
	for i, row := range rows {
		cats[i] = *pgCategoryToDomain(row)
	}
	return cats, nil
}

func (a *PGAdapter) UpdateCategoryGameMetadata(ctx context.Context, id, boxArtURL, igdbID string) error {
	if err := a.queries.UpdateCategoryGameMetadata(ctx, pggen.UpdateCategoryGameMetadataParams{
		ID:      id,
		Column2: boxArtURL,
		Column3: igdbID,
	}); err != nil {
		return fmt.Errorf("pg update category game metadata %s: %w", id, err)
	}
	return nil
}

func (a *PGAdapter) MarkCategoryGameMetadataChecked(ctx context.Context, id string) error {
	if err := a.queries.MarkCategoryGameMetadataChecked(ctx, id); err != nil {
		return fmt.Errorf("pg mark category game metadata checked %s: %w", id, err)
	}
	return nil
}

func (a *PGAdapter) ListCategoriesMissingDescription(ctx context.Context, checkedBefore time.Time) ([]repository.Category, error) {
	rows, err := a.queries.ListCategoriesMissingDescription(ctx, &checkedBefore)
	if err != nil {
		return nil, fmt.Errorf("pg list categories missing description: %w", err)
	}
	cats := make([]repository.Category, len(rows))
	for i, row := range rows {
		cats[i] = *pgCategoryToDomain(row)
	}
	return cats, nil
}

func (a *PGAdapter) UpdateCategoryDescription(ctx context.Context, id, description string) error {
	if err := a.queries.UpdateCategoryDescription(ctx, pggen.UpdateCategoryDescriptionParams{
		ID:          id,
		Description: &description,
	}); err != nil {
		return fmt.Errorf("pg update category description %s: %w", id, err)
	}
	return nil
}

func (a *PGAdapter) MarkCategoryDescriptionChecked(ctx context.Context, id string) error {
	if err := a.queries.MarkCategoryDescriptionChecked(ctx, id); err != nil {
		return fmt.Errorf("pg mark category description checked %s: %w", id, err)
	}
	return nil
}

func (a *PGAdapter) GetCategorySearchCache(ctx context.Context, normalizedQuery string) (*repository.CategorySearchCache, error) {
	row, err := a.queries.GetCategorySearchCache(ctx, normalizedQuery)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgCategorySearchCacheToDomain(row)
}

func (a *PGAdapter) UpsertCategorySearchCache(ctx context.Context, input repository.CategorySearchCacheInput) (*repository.CategorySearchCache, error) {
	categoryIDs, err := json.Marshal(input.CategoryIDs)
	if err != nil {
		return nil, fmt.Errorf("pg marshal category search cache IDs: %w", err)
	}
	row, err := a.queries.UpsertCategorySearchCache(ctx, pggen.UpsertCategorySearchCacheParams{
		NormalizedQuery: input.NormalizedQuery,
		CategoryIds:     categoryIDs,
		ExpiresAt:       input.ExpiresAt,
		LastAccessedAt:  input.LastAccessedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("pg upsert category search cache %q: %w", input.NormalizedQuery, err)
	}
	return pgCategorySearchCacheToDomain(row)
}

func (a *PGAdapter) TouchCategorySearchCache(ctx context.Context, normalizedQuery string, at time.Time) error {
	if err := a.queries.TouchCategorySearchCache(ctx, pggen.TouchCategorySearchCacheParams{
		NormalizedQuery: normalizedQuery,
		LastAccessedAt:  at,
	}); err != nil {
		return fmt.Errorf("pg touch category search cache %q: %w", normalizedQuery, err)
	}
	return nil
}

func (a *PGAdapter) DeleteExpiredCategorySearchCache(ctx context.Context, before time.Time) error {
	if err := a.queries.DeleteExpiredCategorySearchCache(ctx, before); err != nil {
		return fmt.Errorf("pg delete expired category search cache: %w", err)
	}
	return nil
}

func (a *PGAdapter) PruneCategorySearchCache(ctx context.Context, maxRows int) error {
	if maxRows < 0 {
		maxRows = 0
	}
	if err := a.queries.PruneCategorySearchCache(ctx, int32(maxRows)); err != nil {
		return fmt.Errorf("pg prune category search cache: %w", err)
	}
	return nil
}

func pgCategoryCursorName(cursor *repository.CategoryPageCursor) *string {
	if cursor == nil {
		return nil
	}
	return &cursor.Name
}

func pgCategoryCursorID(cursor *repository.CategoryPageCursor) string {
	if cursor == nil {
		return ""
	}
	return cursor.ID
}

func pgCategoryCursorLatestVideoAt(cursor *repository.CategoryPageCursor) *time.Time {
	if cursor == nil {
		return nil
	}
	return cursor.LatestVideoAt
}

func pgCategoryCursorVideoCount(cursor *repository.CategoryPageCursor) int64 {
	if cursor == nil {
		return 0
	}
	return cursor.VideoCount
}

func pgLatestCategoryPageItem(row pggen.ListCategoriesWithVideosPageLatestDescRow) (repository.CategoryPageItem, error) {
	latest, err := pgCategoryPageTime(row.LatestVideoAt)
	if err != nil {
		return repository.CategoryPageItem{}, err
	}
	return repository.CategoryPageItem{
		Category: repository.Category{
			ID:                    row.ID,
			Name:                  row.Name,
			BoxArtURL:             row.BoxArtUrl,
			IGDBID:                row.IgdbID,
			Description:           row.Description,
			GameMetadataCheckedAt: row.GameMetadataCheckedAt,
			DescriptionCheckedAt:  row.DescriptionCheckedAt,
			CreatedAt:             row.CreatedAt,
			UpdatedAt:             row.UpdatedAt,
		},
		LatestVideoAt: latest,
		VideoCount:    row.VideoCount,
	}, nil
}

func pgCountCategoryPageItem(row pggen.ListCategoriesWithVideosPageVideoCountDescRow) (repository.CategoryPageItem, error) {
	latest, err := pgCategoryPageTime(row.LatestVideoAt)
	if err != nil {
		return repository.CategoryPageItem{}, err
	}
	return repository.CategoryPageItem{
		Category: repository.Category{
			ID:                    row.ID,
			Name:                  row.Name,
			BoxArtURL:             row.BoxArtUrl,
			IGDBID:                row.IgdbID,
			Description:           row.Description,
			GameMetadataCheckedAt: row.GameMetadataCheckedAt,
			DescriptionCheckedAt:  row.DescriptionCheckedAt,
			CreatedAt:             row.CreatedAt,
			UpdatedAt:             row.UpdatedAt,
		},
		LatestVideoAt: latest,
		VideoCount:    row.VideoCount,
	}, nil
}

func pgCategoryPageTime(v any) (time.Time, error) {
	switch x := v.(type) {
	case time.Time:
		return x.UTC(), nil
	case *time.Time:
		if x == nil {
			return time.Time{}, fmt.Errorf("pg category page timestamp: nil value")
		}
		return x.UTC(), nil
	case string:
		return time.Parse(time.RFC3339, x)
	case []byte:
		return time.Parse(time.RFC3339, string(x))
	default:
		return time.Time{}, fmt.Errorf("pg category page timestamp: cannot scan %T", v)
	}
}

// Tags

func (a *PGAdapter) GetTag(ctx context.Context, id int64) (*repository.Tag, error) {
	row, err := a.queries.GetTag(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgTagToDomain(row), nil
}

func (a *PGAdapter) GetTagByName(ctx context.Context, name string) (*repository.Tag, error) {
	row, err := a.queries.GetTagByName(ctx, name)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgTagToDomain(row), nil
}

func (a *PGAdapter) ListTags(ctx context.Context) ([]repository.Tag, error) {
	rows, err := a.queries.ListTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg list tags: %w", err)
	}
	tags := make([]repository.Tag, len(rows))
	for i, row := range rows {
		tags[i] = *pgTagToDomain(row)
	}
	return tags, nil
}

func pgCategorySearchCacheToDomain(c pggen.CategorySearchCache) (*repository.CategorySearchCache, error) {
	var categoryIDs []string
	if err := json.Unmarshal(c.CategoryIds, &categoryIDs); err != nil {
		return nil, fmt.Errorf("pg decode category search cache %q IDs: %w", c.NormalizedQuery, err)
	}
	return &repository.CategorySearchCache{
		NormalizedQuery: c.NormalizedQuery,
		CategoryIDs:     categoryIDs,
		ExpiresAt:       c.ExpiresAt,
		LastAccessedAt:  c.LastAccessedAt,
		CreatedAt:       c.CreatedAt,
		UpdatedAt:       c.UpdatedAt,
	}, nil
}
