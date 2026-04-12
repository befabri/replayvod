package system

import (
	"context"
	"errors"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/trpcgo"
)

// UserInfo is the wire shape for an admin-visible user record.
type UserInfo struct {
	ID              string    `json:"id"`
	Login           string    `json:"login"`
	DisplayName     string    `json:"display_name"`
	Email           *string   `json:"email,omitempty"`
	ProfileImageURL *string   `json:"profile_image_url,omitempty"`
	Role            string    `json:"role"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func toUserInfo(u *repository.User) UserInfo {
	return UserInfo{
		ID:              u.ID,
		Login:           u.Login,
		DisplayName:     u.DisplayName,
		Email:           u.Email,
		ProfileImageURL: u.ProfileImageURL,
		Role:            u.Role,
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

// ListUsers returns every user.
func (s *Service) ListUsers(ctx context.Context) ([]UserInfo, error) {
	users, err := s.repo.ListUsers(ctx)
	if err != nil {
		s.log.Error("failed to list users", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list users")
	}
	out := make([]UserInfo, len(users))
	for i, u := range users {
		out[i] = toUserInfo(&u)
	}
	return out, nil
}

// UpdateUserRoleInput assigns a role to a user. The validator enforces the
// enum so clients can't smuggle arbitrary values into the DB.
type UpdateUserRoleInput struct {
	UserID string `json:"user_id" validate:"required"`
	Role   string `json:"role" validate:"required,oneof=viewer admin owner"`
}

// UpdateUserRole changes a user's role. The owner cannot demote themselves —
// that would lock the system out of any ownership-gated procedure.
func (s *Service) UpdateUserRole(ctx context.Context, input UpdateUserRoleInput) (UserInfo, error) {
	caller := middleware.GetUser(ctx)
	if caller == nil {
		return UserInfo{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	if caller.ID == input.UserID && input.Role != "owner" {
		return UserInfo{}, trpcgo.NewError(trpcgo.CodeBadRequest, "cannot demote yourself")
	}

	if err := s.repo.UpdateUserRole(ctx, input.UserID, input.Role); err != nil {
		s.log.Error("failed to update user role", "user_id", input.UserID, "error", err)
		return UserInfo{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to update user role")
	}

	u, err := s.repo.GetUser(ctx, input.UserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return UserInfo{}, trpcgo.NewError(trpcgo.CodeNotFound, "user not found")
		}
		s.log.Error("failed to reload user after role update", "error", err)
		return UserInfo{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load user")
	}
	return toUserInfo(u), nil
}
