package system

import (
	"context"
	"time"

	"github.com/befabri/trpcgo"
)

type WhitelistEntryInfo struct {
	TwitchUserID string    `json:"twitch_user_id"`
	AddedAt      time.Time `json:"added_at"`
}

func (s *Service) ListWhitelist(ctx context.Context) ([]WhitelistEntryInfo, error) {
	entries, err := s.svc.ListWhitelist(ctx)
	if err != nil {
		s.log.Error("list whitelist", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list whitelist")
	}
	out := make([]WhitelistEntryInfo, len(entries))
	for i, e := range entries {
		out[i] = WhitelistEntryInfo{TwitchUserID: e.TwitchUserID, AddedAt: e.AddedAt}
	}
	return out, nil
}

type WhitelistIDInput struct {
	TwitchUserID string `json:"twitch_user_id" validate:"required"`
}

type OK struct {
	OK bool `json:"ok"`
}

func (s *Service) AddWhitelist(ctx context.Context, input WhitelistIDInput) (OK, error) {
	if err := s.svc.AddToWhitelist(ctx, input.TwitchUserID); err != nil {
		s.log.Error("add whitelist entry", "twitch_user_id", input.TwitchUserID, "error", err)
		return OK{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to add to whitelist")
	}
	return OK{OK: true}, nil
}

func (s *Service) RemoveWhitelist(ctx context.Context, input WhitelistIDInput) (OK, error) {
	if err := s.svc.RemoveFromWhitelist(ctx, input.TwitchUserID); err != nil {
		s.log.Error("remove whitelist entry", "twitch_user_id", input.TwitchUserID, "error", err)
		return OK{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to remove from whitelist")
	}
	return OK{OK: true}, nil
}
