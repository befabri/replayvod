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

func (a *PGAdapter) GetCategoryByName(ctx context.Context, name string) (*repository.Category, error) {
	row, err := a.queries.GetCategoryByName(ctx, name)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgCategoryToDomain(row), nil
}

func (a *PGAdapter) UpsertCategory(ctx context.Context, c *repository.Category) (*repository.Category, error) {
	row, err := a.queries.UpsertCategory(ctx, pggen.UpsertCategoryParams{
		ID:        c.ID,
		Name:      c.Name,
		BoxArtUrl: c.BoxArtURL,
		IgdbID:    c.IGDBID,
	})
	if err != nil {
		return nil, fmt.Errorf("pg upsert category %s: %w", c.ID, err)
	}
	return pgCategoryToDomain(row), nil
}

const upsertCategoriesSQL = `WITH input AS (
    SELECT *
    FROM unnest($1::text[], $2::text[], $3::text[], $4::text[]) WITH ORDINALITY
        AS t(id, name, box_art_url, igdb_id, ord)
),
upserted AS (
    INSERT INTO categories (id, name, box_art_url, igdb_id)
    SELECT id, name, box_art_url, igdb_id
    FROM input
    ON CONFLICT (id) DO UPDATE SET
        name = EXCLUDED.name,
        box_art_url = COALESCE(NULLIF(EXCLUDED.box_art_url, ''), categories.box_art_url),
        igdb_id = COALESCE(NULLIF(EXCLUDED.igdb_id, ''), categories.igdb_id),
        updated_at = NOW()
    RETURNING id, name, box_art_url, igdb_id, created_at, updated_at
)
SELECT u.id, u.name, u.box_art_url, u.igdb_id, u.created_at, u.updated_at
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
	for i, c := range categories {
		ids[i] = c.ID
		names[i] = c.Name
		boxArtURLs[i] = c.BoxArtURL
		igdbIDs[i] = c.IGDBID
	}

	rows, err := a.db.Query(ctx, upsertCategoriesSQL, ids, names, boxArtURLs, igdbIDs)
	if err != nil {
		return nil, fmt.Errorf("pg upsert categories batch: %w", err)
	}
	defer rows.Close()

	out := []repository.Category{}
	for rows.Next() {
		var row pggen.Category
		if err := rows.Scan(&row.ID, &row.Name, &row.BoxArtUrl, &row.IgdbID, &row.CreatedAt, &row.UpdatedAt); err != nil {
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

func (a *PGAdapter) ListCategoriesMissingBoxArt(ctx context.Context) ([]repository.Category, error) {
	rows, err := a.queries.ListCategoriesMissingBoxArt(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg list categories missing box art: %w", err)
	}
	cats := make([]repository.Category, len(rows))
	for i, row := range rows {
		cats[i] = *pgCategoryToDomain(row)
	}
	return cats, nil
}

func (a *PGAdapter) UpdateCategoryBoxArt(ctx context.Context, id, boxArtURL string) error {
	if err := a.queries.UpdateCategoryBoxArt(ctx, pggen.UpdateCategoryBoxArtParams{
		ID:        id,
		BoxArtUrl: &boxArtURL,
	}); err != nil {
		return fmt.Errorf("pg update category box art %s: %w", id, err)
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

func (a *PGAdapter) UpsertTag(ctx context.Context, name string) (*repository.Tag, error) {
	row, err := a.queries.UpsertTag(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("pg upsert tag %s: %w", name, err)
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

func pgCategoryToDomain(c pggen.Category) *repository.Category {
	return &repository.Category{
		ID:        c.ID,
		Name:      c.Name,
		BoxArtURL: c.BoxArtUrl,
		IGDBID:    c.IgdbID,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
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

func pgTagToDomain(t pggen.Tag) *repository.Tag {
	return &repository.Tag{
		ID:        t.ID,
		Name:      t.Name,
		CreatedAt: t.CreatedAt,
	}
}
