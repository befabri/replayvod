package system

import (
	"context"
	"time"

	"github.com/befabri/replayvod/server/internal/server/api/apierr"
)

type WhitelistEntryInfo struct {
	TwitchUserID string    `json:"twitch_user_id"`
	AddedAt      time.Time `json:"added_at"`
}

func (h *Handler) ListWhitelist(ctx context.Context) ([]WhitelistEntryInfo, error) {
	entries, err := h.svc.ListWhitelist(ctx)
	if err != nil {
		return nil, apierr.Map(h.log, err, "list whitelist")
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

func (h *Handler) AddWhitelist(ctx context.Context, input WhitelistIDInput) (OK, error) {
	if err := h.svc.AddToWhitelist(ctx, input.TwitchUserID); err != nil {
		return OK{}, apierr.Map(h.log, err, "add to whitelist")
	}
	return OK{OK: true}, nil
}

func (h *Handler) RemoveWhitelist(ctx context.Context, input WhitelistIDInput) (OK, error) {
	if err := h.svc.RemoveFromWhitelist(ctx, input.TwitchUserID); err != nil {
		return OK{}, apierr.Map(h.log, err, "remove from whitelist")
	}
	return OK{OK: true}, nil
}
