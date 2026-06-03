package system

import (
	"context"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/apierr"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/trpcgo"
)

type UserInfo struct {
	ID              string          `json:"id"`
	Login           string          `json:"login"`
	DisplayName     string          `json:"display_name"`
	Email           *string         `json:"email,omitempty"`
	ProfileImageURL *string         `json:"profile_image_url,omitempty"`
	Role            middleware.Role `json:"role"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

func toUserInfo(u *repository.User) UserInfo {
	return UserInfo{
		ID:              u.ID,
		Login:           u.Login,
		DisplayName:     u.DisplayName,
		Email:           u.Email,
		ProfileImageURL: u.ProfileImageURL,
		Role:            middleware.Role(u.Role),
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

func (h *Handler) ListUsers(ctx context.Context) ([]UserInfo, error) {
	users, err := h.svc.ListUsers(ctx)
	if err != nil {
		return nil, apierr.Map(h.log, err, "list users")
	}
	out := make([]UserInfo, len(users))
	for i := range users {
		out[i] = toUserInfo(&users[i])
	}
	return out, nil
}

type UpdateUserRoleInput struct {
	UserID string `json:"user_id" validate:"required"`
	Role   string `json:"role" validate:"required,oneof=viewer admin owner"`
}

func (h *Handler) UpdateUserRole(ctx context.Context, input UpdateUserRoleInput) (UserInfo, error) {
	caller, err := middleware.RequireUser(ctx)
	if err != nil {
		return UserInfo{}, err
	}
	u, err := h.svc.UpdateUserRole(ctx, caller.ID, input.UserID, input.Role)
	if err != nil {
		return UserInfo{}, apierr.Map(h.log, err, "update user role",
			apierr.On(ErrCannotDemoteSelf, trpcgo.CodeBadRequest, "cannot demote yourself"))
	}
	return toUserInfo(u), nil
}
