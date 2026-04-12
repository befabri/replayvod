package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

// SQLiteAdapter implements repository.Repository using SQLite via sqlc-generated code.
type SQLiteAdapter struct {
	queries *sqlitegen.Queries
}

// New creates a new SQLiteAdapter.
func New(queries *sqlitegen.Queries) *SQLiteAdapter {
	return &SQLiteAdapter{queries: queries}
}

func (a *SQLiteAdapter) GetUser(ctx context.Context, id string) (*repository.User, error) {
	row, err := a.queries.GetUser(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("sqlite get user %s: %w", id, err)
	}
	return sqliteUserToDomain(row), nil
}

func (a *SQLiteAdapter) GetUserByLogin(ctx context.Context, login string) (*repository.User, error) {
	row, err := a.queries.GetUserByLogin(ctx, login)
	if err != nil {
		return nil, fmt.Errorf("sqlite get user by login %s: %w", login, err)
	}
	return sqliteUserToDomain(row), nil
}

func (a *SQLiteAdapter) UpsertUser(ctx context.Context, u *repository.User) (*repository.User, error) {
	row, err := a.queries.UpsertUser(ctx, sqlitegen.UpsertUserParams{
		ID:              u.ID,
		Login:           u.Login,
		DisplayName:     u.DisplayName,
		Email:           toNullString(u.Email),
		ProfileImageUrl: toNullString(u.ProfileImageURL),
		Role:            u.Role,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert user %s: %w", u.ID, err)
	}
	return sqliteUserToDomain(row), nil
}

func (a *SQLiteAdapter) ListUsers(ctx context.Context) ([]repository.User, error) {
	rows, err := a.queries.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite list users: %w", err)
	}
	users := make([]repository.User, len(rows))
	for i, row := range rows {
		users[i] = *sqliteUserToDomain(row)
	}
	return users, nil
}

func (a *SQLiteAdapter) UpdateUserRole(ctx context.Context, id string, role string) error {
	if err := a.queries.UpdateUserRole(ctx, sqlitegen.UpdateUserRoleParams{
		ID:   id,
		Role: role,
	}); err != nil {
		return fmt.Errorf("sqlite update user role %s: %w", id, err)
	}
	return nil
}

// Conversion helpers

func sqliteUserToDomain(u sqlitegen.User) *repository.User {
	return &repository.User{
		ID:              u.ID,
		Login:           u.Login,
		DisplayName:     u.DisplayName,
		Email:           fromNullString(u.Email),
		ProfileImageURL: fromNullString(u.ProfileImageUrl),
		Role:            u.Role,
		CreatedAt:       parseTime(u.CreatedAt),
		UpdatedAt:       parseTime(u.UpdatedAt),
	}
}

func fromNullString(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}

func toNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func parseTime(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}
