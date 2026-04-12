package system

import (
	"context"
	"time"

	"github.com/befabri/trpcgo"
)

// WhitelistEntryInfo is the wire shape for a whitelist row.
type WhitelistEntryInfo struct {
	TwitchUserID string    `json:"twitch_user_id"`
	AddedAt      time.Time `json:"added_at"`
}

// ListWhitelist returns all whitelisted Twitch user IDs.
func (s *Service) ListWhitelist(ctx context.Context) ([]WhitelistEntryInfo, error) {
	entries, err := s.repo.ListWhitelist(ctx)
	if err != nil {
		s.log.Error("failed to list whitelist", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list whitelist")
	}
	out := make([]WhitelistEntryInfo, len(entries))
	for i, e := range entries {
		out[i] = WhitelistEntryInfo{TwitchUserID: e.TwitchUserID, AddedAt: e.AddedAt}
	}
	return out, nil
}

// WhitelistIDInput holds a Twitch user ID. numeric=false — Twitch IDs are
// documented strings and we want to pass them through untouched.
type WhitelistIDInput struct {
	TwitchUserID string `json:"twitch_user_id" validate:"required"`
}

// OK is a minimal ack response.
type OK struct {
	OK bool `json:"ok"`
}

// AddWhitelist adds a Twitch user ID to the whitelist (idempotent).
func (s *Service) AddWhitelist(ctx context.Context, input WhitelistIDInput) (OK, error) {
	if err := s.repo.AddToWhitelist(ctx, input.TwitchUserID); err != nil {
		s.log.Error("failed to add whitelist entry", "twitch_user_id", input.TwitchUserID, "error", err)
		return OK{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to add to whitelist")
	}
	return OK{OK: true}, nil
}

// RemoveWhitelist removes a Twitch user ID from the whitelist.
func (s *Service) RemoveWhitelist(ctx context.Context, input WhitelistIDInput) (OK, error) {
	if err := s.repo.RemoveFromWhitelist(ctx, input.TwitchUserID); err != nil {
		s.log.Error("failed to remove whitelist entry", "twitch_user_id", input.TwitchUserID, "error", err)
		return OK{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to remove from whitelist")
	}
	return OK{OK: true}, nil
}
