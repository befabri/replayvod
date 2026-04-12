package pgadapter

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

// PGAdapter implements repository.Repository using PostgreSQL via sqlc-generated code.
type PGAdapter struct {
	queries *pggen.Queries
}

// New creates a new PGAdapter.
func New(queries *pggen.Queries) *PGAdapter {
	return &PGAdapter{queries: queries}
}

func (a *PGAdapter) GetUser(ctx context.Context, id string) (*repository.User, error) {
	row, err := a.queries.GetUser(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("pg get user %s: %w", id, err)
	}
	return pgUserToDomain(row), nil
}

func (a *PGAdapter) GetUserByLogin(ctx context.Context, login string) (*repository.User, error) {
	row, err := a.queries.GetUserByLogin(ctx, login)
	if err != nil {
		return nil, fmt.Errorf("pg get user by login %s: %w", login, err)
	}
	return pgUserToDomain(row), nil
}

func (a *PGAdapter) UpsertUser(ctx context.Context, u *repository.User) (*repository.User, error) {
	row, err := a.queries.UpsertUser(ctx, pggen.UpsertUserParams{
		ID:              u.ID,
		Login:           u.Login,
		DisplayName:     u.DisplayName,
		Email:           toPgText(u.Email),
		ProfileImageUrl: toPgText(u.ProfileImageURL),
		Role:            u.Role,
	})
	if err != nil {
		return nil, fmt.Errorf("pg upsert user %s: %w", u.ID, err)
	}
	return pgUserToDomain(row), nil
}

func (a *PGAdapter) ListUsers(ctx context.Context) ([]repository.User, error) {
	rows, err := a.queries.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg list users: %w", err)
	}
	users := make([]repository.User, len(rows))
	for i, row := range rows {
		users[i] = *pgUserToDomain(row)
	}
	return users, nil
}

func (a *PGAdapter) UpdateUserRole(ctx context.Context, id string, role string) error {
	if err := a.queries.UpdateUserRole(ctx, pggen.UpdateUserRoleParams{
		ID:   id,
		Role: role,
	}); err != nil {
		return fmt.Errorf("pg update user role %s: %w", id, err)
	}
	return nil
}

// Conversion helpers

func pgUserToDomain(u pggen.User) *repository.User {
	return &repository.User{
		ID:              u.ID,
		Login:           u.Login,
		DisplayName:     u.DisplayName,
		Email:           fromPgText(u.Email),
		ProfileImageURL: fromPgText(u.ProfileImageUrl),
		Role:            u.Role,
		CreatedAt:       u.CreatedAt.Time,
		UpdatedAt:       u.UpdatedAt.Time,
	}
}

func fromPgText(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func toPgText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}
