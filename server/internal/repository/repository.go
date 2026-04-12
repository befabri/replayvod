package repository

import "context"

// Repository is the common interface for database access.
// Both PG and SQLite adapters implement this.
type Repository interface {
	// Users
	GetUser(ctx context.Context, id string) (*User, error)
	GetUserByLogin(ctx context.Context, login string) (*User, error)
	UpsertUser(ctx context.Context, u *User) (*User, error)
	ListUsers(ctx context.Context) ([]User, error)
	UpdateUserRole(ctx context.Context, id string, role string) error
}
