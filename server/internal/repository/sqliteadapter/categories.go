package sqliteadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

// Categories

func (a *SQLiteAdapter) GetCategory(ctx context.Context, id string) (*repository.Category, error) {
	row, err := a.queries.GetCategory(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteCategoryToDomain(row), nil
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
		ID:        c.ID,
		Name:      c.Name,
		BoxArtUrl: toNullString(c.BoxArtURL),
		IgdbID:    toNullString(c.IGDBID),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert category %s: %w", c.ID, err)
	}
	return sqliteCategoryToDomain(row), nil
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

// searchCategoriesSQL mirrors queries/postgres/categories.sql
// SearchCategories. Hand-rolled because sqlc's SQLite engine can't
// type-infer a ?N param reused across WHERE and CASE branches — same
// limitation we hit for SearchChannels and ListVideos. Keeps the
// ranking contract identical across dialects.
const searchCategoriesSQL = `SELECT
    id, name, box_art_url, igdb_id, created_at, updated_at
FROM categories
WHERE ?1 = ''
   OR lower(name) LIKE '%' || lower(?1) || '%'
ORDER BY
    CASE
        WHEN ?1 = '' THEN 3
        WHEN lower(name) = lower(?1) THEN 0
        WHEN lower(name) LIKE lower(?1) || '%' THEN 1
        ELSE 2
    END,
    name
LIMIT ?2`

func (a *SQLiteAdapter) SearchCategories(ctx context.Context, query string, limit int) ([]repository.Category, error) {
	rows, err := a.db.QueryContext(ctx, searchCategoriesSQL, query, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("sqlite search categories: %w", err)
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
			&row.CreatedAt,
			&row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite search categories scan: %w", err)
		}
		out = append(out, *sqliteCategoryToDomain(row))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite search categories: %w", err)
	}
	return out, nil
}

func (a *SQLiteAdapter) ListCategoriesMissingBoxArt(ctx context.Context) ([]repository.Category, error) {
	rows, err := a.queries.ListCategoriesMissingBoxArt(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite list categories missing box art: %w", err)
	}
	cats := make([]repository.Category, len(rows))
	for i, row := range rows {
		cats[i] = *sqliteCategoryToDomain(row)
	}
	return cats, nil
}

func (a *SQLiteAdapter) UpdateCategoryBoxArt(ctx context.Context, id, boxArtURL string) error {
	if err := a.queries.UpdateCategoryBoxArt(ctx, sqlitegen.UpdateCategoryBoxArtParams{
		ID:        id,
		BoxArtUrl: toNullString(&boxArtURL),
	}); err != nil {
		return fmt.Errorf("sqlite update category box art %s: %w", id, err)
	}
	return nil
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
		ID:        c.ID,
		Name:      c.Name,
		BoxArtURL: fromNullString(c.BoxArtUrl),
		IGDBID:    fromNullString(c.IgdbID),
		CreatedAt: parseTime(c.CreatedAt),
		UpdatedAt: parseTime(c.UpdatedAt),
	}
}

func sqliteTagToDomain(t sqlitegen.Tag) *repository.Tag {
	return &repository.Tag{
		ID:        t.ID,
		Name:      t.Name,
		CreatedAt: parseTime(t.CreatedAt),
	}
}
