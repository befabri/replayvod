package pgadapter

import (
	"context"
	"fmt"

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

func pgTagToDomain(t pggen.Tag) *repository.Tag {
	return &repository.Tag{
		ID:        t.ID,
		Name:      t.Name,
		CreatedAt: t.CreatedAt,
	}
}
